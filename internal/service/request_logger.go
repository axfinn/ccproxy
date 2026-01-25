package service

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"ccproxy/internal/store"
)

const (
	DefaultBufferSize = 10000
	DefaultWorkers    = 4
	DefaultBatchSize  = 100
	FlushInterval     = 5 * time.Second
)

type RequestLogger struct {
	store      *store.Store
	queue      chan *LogEntry
	bufferSize int
	workers    int
	batchSize  int
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	running    bool
}

type LogEntry struct {
	Log          *store.RequestLog
	Conversation *store.ConversationContent
}

// NewRequestLogger creates a new request logger with specified buffer size and workers
func NewRequestLogger(store *store.Store, bufferSize, workers int) *RequestLogger {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	if workers <= 0 {
		workers = DefaultWorkers
	}

	return &RequestLogger{
		store:      store,
		queue:      make(chan *LogEntry, bufferSize),
		bufferSize: bufferSize,
		workers:    workers,
		batchSize:  DefaultBatchSize,
		running:    false,
	}
}

// Start starts the request logger workers
func (rl *RequestLogger) Start(ctx context.Context) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.running {
		return nil
	}

	rl.ctx, rl.cancel = context.WithCancel(ctx)
	rl.running = true

	// Start worker goroutines
	for i := 0; i < rl.workers; i++ {
		rl.wg.Add(1)
		go rl.processQueue(i)
	}

	log.Info().
		Int("buffer_size", rl.bufferSize).
		Int("workers", rl.workers).
		Int("batch_size", rl.batchSize).
		Msg("Request logger started")

	return nil
}

// Stop gracefully stops the request logger and flushes remaining logs
func (rl *RequestLogger) Stop() error {
	rl.mu.Lock()
	if !rl.running {
		rl.mu.Unlock()
		return nil
	}
	rl.running = false
	rl.mu.Unlock()

	log.Info().Msg("Stopping request logger...")

	// Cancel context to signal workers to stop
	rl.cancel()

	// Close queue to stop accepting new entries
	close(rl.queue)

	// Wait for all workers to finish
	rl.wg.Wait()

	log.Info().Msg("Request logger stopped")
	return nil
}

// LogRequest queues a log entry for processing (non-blocking)
func (rl *RequestLogger) LogRequest(entry *LogEntry) error {
	rl.mu.Lock()
	running := rl.running
	rl.mu.Unlock()

	if !running {
		log.Warn().Msg("Request logger not running, skipping log entry")
		return nil
	}

	select {
	case rl.queue <- entry:
		return nil
	default:
		// Queue is full, log warning and drop oldest entry
		log.Warn().
			Int("queue_size", len(rl.queue)).
			Int("buffer_size", rl.bufferSize).
			Msg("Request log queue full, dropping entry")
		return nil
	}
}

// processQueue is a worker goroutine that processes log entries from the queue
func (rl *RequestLogger) processQueue(workerID int) {
	defer rl.wg.Done()

	batch := make([]*LogEntry, 0, rl.batchSize)
	ticker := time.NewTicker(FlushInterval)
	defer ticker.Stop()

	log.Debug().Int("worker_id", workerID).Msg("Request logger worker started")

	for {
		select {
		case entry, ok := <-rl.queue:
			if !ok {
				// Queue closed, flush remaining batch and exit
				if len(batch) > 0 {
					rl.writeBatch(batch)
				}
				log.Debug().Int("worker_id", workerID).Msg("Request logger worker stopped")
				return
			}

			batch = append(batch, entry)

			// Write batch when it reaches batch size
			if len(batch) >= rl.batchSize {
				rl.writeBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// Periodically flush batch even if not full
			if len(batch) > 0 {
				rl.writeBatch(batch)
				batch = batch[:0]
			}

		case <-rl.ctx.Done():
			// Context cancelled, flush remaining batch and exit
			if len(batch) > 0 {
				rl.writeBatch(batch)
			}
			log.Debug().Int("worker_id", workerID).Msg("Request logger worker stopped")
			return
		}
	}
}

// writeBatch writes a batch of log entries to the database
func (rl *RequestLogger) writeBatch(entries []*LogEntry) {
	if len(entries) == 0 {
		return
	}

	start := time.Now()

	// Separate request logs and conversations
	requestLogs := make([]*store.RequestLog, 0, len(entries))
	conversations := make([]*store.ConversationContent, 0)

	for _, entry := range entries {
		if entry.Log != nil {
			requestLogs = append(requestLogs, entry.Log)
		}
		if entry.Conversation != nil {
			conversations = append(conversations, entry.Conversation)
		}
	}

	// Write request logs
	if len(requestLogs) > 0 {
		if err := rl.batchInsertRequestLogs(requestLogs); err != nil {
			log.Error().Err(err).Int("count", len(requestLogs)).Msg("Failed to batch insert request logs")
		} else {
			log.Debug().Int("count", len(requestLogs)).Dur("duration", time.Since(start)).Msg("Batch inserted request logs")
		}
	}

	// Write conversations
	if len(conversations) > 0 {
		if err := rl.batchInsertConversations(conversations); err != nil {
			log.Error().Err(err).Int("count", len(conversations)).Msg("Failed to batch insert conversations")
		} else {
			log.Debug().Int("count", len(conversations)).Dur("duration", time.Since(start)).Msg("Batch inserted conversations")
		}
	}
}

// batchInsertRequestLogs inserts multiple request logs in a single transaction
func (rl *RequestLogger) batchInsertRequestLogs(logs []*store.RequestLog) error {
	tx, err := rl.store.GetDB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO request_logs (
		id, token_id, account_id, user_name, mode, model, stream,
		request_at, response_at, duration_ms, ttft_ms,
		prompt_tokens, completion_tokens, total_tokens,
		status_code, success, error_message, conversation_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, reqLog := range logs {
		_, err = stmt.Exec(
			reqLog.ID, reqLog.TokenID, reqLog.AccountID, reqLog.UserName, reqLog.Mode, reqLog.Model, reqLog.Stream,
			reqLog.RequestAt, reqLog.ResponseAt, reqLog.DurationMs, reqLog.TTFTMs,
			reqLog.PromptTokens, reqLog.CompletionTokens, reqLog.TotalTokens,
			reqLog.StatusCode, reqLog.Success, reqLog.ErrorMessage, reqLog.ConversationID,
		)
		if err != nil {
			log.Error().Err(err).Str("log_id", reqLog.ID).Msg("Failed to insert request log")
			continue
		}
	}

	return tx.Commit()
}

// batchInsertConversations inserts multiple conversations in a single transaction
func (rl *RequestLogger) batchInsertConversations(conversations []*store.ConversationContent) error {
	tx, err := rl.store.GetDB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO conversation_contents (
		id, request_log_id, token_id, system_prompt, messages_json,
		prompt, completion, created_at, is_compressed
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	ftsStmt, err := tx.Prepare(`INSERT INTO conversation_search (id, prompt, completion) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ftsStmt.Close()

	for _, conv := range conversations {
		_, err = stmt.Exec(
			conv.ID, conv.RequestLogID, conv.TokenID, conv.SystemPrompt, conv.MessagesJSON,
			conv.Prompt, conv.Completion, conv.CreatedAt, conv.IsCompressed,
		)
		if err != nil {
			log.Error().Err(err).Str("conv_id", conv.ID).Msg("Failed to insert conversation")
			continue
		}

		// Also insert into FTS index
		_, err = ftsStmt.Exec(conv.ID, conv.Prompt, conv.Completion)
		if err != nil {
			log.Error().Err(err).Str("conv_id", conv.ID).Msg("Failed to insert conversation into FTS index")
		}
	}

	return tx.Commit()
}

// GetQueueStatus returns the current queue size and capacity
func (rl *RequestLogger) GetQueueStatus() (size, capacity int) {
	return len(rl.queue), rl.bufferSize
}
