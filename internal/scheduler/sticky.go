package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// StickyHashOptions contains options for generating a sticky session hash
type StickyHashOptions struct {
	UserID        string   // Highest priority: user ID from metadata
	SystemPrompt  string   // Second priority: system prompt
	Messages      []string // Third priority: first user message
}

// GenerateStickyHash generates a sticky session hash from the given options
// Priority: metadata.user_id > system prompt > first user message
func GenerateStickyHash(opts StickyHashOptions) string {
	var hashInput string

	// Priority 1: User ID from metadata
	if opts.UserID != "" {
		hashInput = "user:" + opts.UserID
	} else if opts.SystemPrompt != "" {
		// Priority 2: System prompt (truncated for consistency)
		hashInput = "system:" + truncateForHash(opts.SystemPrompt, 512)
	} else if len(opts.Messages) > 0 && opts.Messages[0] != "" {
		// Priority 3: First user message
		hashInput = "message:" + truncateForHash(opts.Messages[0], 256)
	} else {
		// No suitable input, return empty
		return ""
	}

	hash := sha256.Sum256([]byte(hashInput))
	return hex.EncodeToString(hash[:])
}

// truncateForHash truncates a string for hashing
func truncateForHash(s string, maxLen int) string {
	// Normalize whitespace
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")

	// Truncate if needed
	if len(s) > maxLen {
		s = s[:maxLen]
	}

	return s
}

// ExtractStickyInfo extracts sticky session info from OpenAI-format messages
func ExtractStickyInfo(messages []map[string]interface{}) StickyHashOptions {
	var opts StickyHashOptions

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		switch role {
		case "system":
			if opts.SystemPrompt == "" {
				opts.SystemPrompt = content
			}
		case "user":
			if len(opts.Messages) == 0 {
				opts.Messages = append(opts.Messages, content)
			}
		}
	}

	return opts
}

// ExtractStickyInfoFromAnthropic extracts sticky session info from Anthropic-format request
func ExtractStickyInfoFromAnthropic(system string, messages []map[string]interface{}) StickyHashOptions {
	opts := StickyHashOptions{
		SystemPrompt: system,
	}

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		if role == "user" && len(opts.Messages) == 0 {
			opts.Messages = append(opts.Messages, content)
			break
		}
	}

	return opts
}
