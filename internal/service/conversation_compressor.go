package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"ccproxy/internal/store"
)

const (
	DefaultCompressAge          = 7 * 24 * time.Hour // Compress conversations older than 7 days
	DefaultCompressInterval     = 24 * time.Hour     // Run compression daily
	DefaultCompressBatchSize    = 100                // Compress 100 conversations per batch
)

type ConversationCompressor struct {
	store        *store.Store
	compressAge  time.Duration
	interval     time.Duration
	batchSize    int
	ticker       *time.Ticker
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	running      bool
}

// NewConversationCompressor creates a new conversation compressor
func NewConversationCompressor(store *store.Store, compressAge, interval time.Duration) *ConversationCompressor {
	if compressAge <= 0 {
		compressAge = DefaultCompressAge
	}
	if interval <= 0 {
		interval = DefaultCompressInterval
	}

	return &ConversationCompressor{
		store:       store,
		compressAge: compressAge,
		interval:    interval,
		batchSize:   DefaultCompressBatchSize,
	}
}

// Start starts the conversation compressor background task
func (cc *ConversationCompressor) Start(ctx context.Context) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if cc.running {
		return nil
	}

	cc.ctx, cc.cancel = context.WithCancel(ctx)
	cc.ticker = time.NewTicker(cc.interval)
	cc.running = true

	// Run compression immediately on start
	go func() {
		if err := cc.runCompression(); err != nil {
			log.Error().Err(err).Msg("Initial conversation compression failed")
		}
	}()

	// Start background worker
	cc.wg.Add(1)
	go cc.worker()

	log.Info().Dur("compress_age", cc.compressAge).Dur("interval", cc.interval).Msg("Conversation compressor started")
	return nil
}

// Stop stops the conversation compressor
func (cc *ConversationCompressor) Stop() error {
	cc.mu.Lock()
	if !cc.running {
		cc.mu.Unlock()
		return nil
	}
	cc.running = false
	cc.mu.Unlock()

	log.Info().Msg("Stopping conversation compressor...")

	// Cancel context and stop ticker
	cc.cancel()
	cc.ticker.Stop()

	// Wait for worker to finish
	cc.wg.Wait()

	log.Info().Msg("Conversation compressor stopped")
	return nil
}

// worker runs the compression task periodically
func (cc *ConversationCompressor) worker() {
	defer cc.wg.Done()

	for {
		select {
		case <-cc.ticker.C:
			if err := cc.runCompression(); err != nil {
				log.Error().Err(err).Msg("Conversation compression failed")
			}
		case <-cc.ctx.Done():
			return
		}
	}
}

// runCompression compresses old conversations
func (cc *ConversationCompressor) runCompression() error {
	start := time.Now()
	olderThanDays := int(cc.compressAge.Hours() / 24)

	log.Info().Int("older_than_days", olderThanDays).Msg("Running conversation compression")

	totalCompressed := 0
	for {
		// Get batch of uncompressed conversations
		conversations, err := cc.store.GetUncompressedConversations(olderThanDays, cc.batchSize)
		if err != nil {
			return err
		}

		if len(conversations) == 0 {
			break
		}

		// Compress each conversation
		for _, conv := range conversations {
			if err := cc.compressConversation(conv); err != nil {
				log.Error().Err(err).Str("conv_id", conv.ID).Msg("Failed to compress conversation")
				continue
			}
			totalCompressed++
		}

		log.Debug().Int("batch_size", len(conversations)).Int("total", totalCompressed).Msg("Compressed batch")
	}

	duration := time.Since(start)
	log.Info().Int("total_compressed", totalCompressed).Dur("duration", duration).Msg("Conversation compression completed")

	return nil
}

// compressConversation compresses a single conversation's text fields
func (cc *ConversationCompressor) compressConversation(conv *store.ConversationContent) error {
	// Compress prompt
	compressedPrompt, err := compressString(conv.Prompt)
	if err != nil {
		return err
	}

	// Compress completion
	compressedCompletion, err := compressString(conv.Completion)
	if err != nil {
		return err
	}

	// Compress messages JSON
	compressedMessages, err := compressString(conv.MessagesJSON)
	if err != nil {
		return err
	}

	// Compress system prompt if present
	var compressedSystemPrompt string
	if conv.SystemPrompt.Valid {
		compressedSystemPrompt, err = compressString(conv.SystemPrompt.String)
		if err != nil {
			return err
		}
	}

	// Update conversation in database with compressed data
	query := `UPDATE conversation_contents
		SET prompt = ?,
		    completion = ?,
		    messages_json = ?,
		    system_prompt = ?,
		    is_compressed = 1
		WHERE id = ?`

	_, err = cc.store.GetDB().Exec(query,
		compressedPrompt,
		compressedCompletion,
		compressedMessages,
		compressedSystemPrompt,
		conv.ID,
	)

	return err
}

// compressString compresses a string using gzip and returns base64 encoded result
func compressString(s string) (string, error) {
	if s == "" {
		return "", nil
	}

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)

	_, err := writer.Write([]byte(s))
	if err != nil {
		return "", err
	}

	if err := writer.Close(); err != nil {
		return "", err
	}

	// Encode to base64 for safe storage
	compressed := base64.StdEncoding.EncodeToString(buf.Bytes())
	return compressed, nil
}

// DecompressString decompresses a base64 encoded gzip string
func DecompressString(compressed string) (string, error) {
	if compressed == "" {
		return "", nil
	}

	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(compressed)
	if err != nil {
		return "", err
	}

	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(decompressed), nil
}
