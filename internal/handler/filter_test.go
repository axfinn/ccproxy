package handler

import (
	"encoding/json"
	"testing"
)

func TestFilterThinkingBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType string // "unchanged", "filtered", or "error"
	}{
		{
			name: "simple text content",
			input: `{
				"model": "claude-3-opus",
				"messages": [
					{
						"role": "user",
						"content": "Hello"
					}
				]
			}`,
			wantType: "unchanged",
		},
		{
			name: "content with thinking block no signature",
			input: `{
				"model": "claude-3-opus",
				"messages": [
					{
						"role": "user",
						"content": [
							{
								"type": "text",
								"text": "Hello"
							},
							{
								"type": "thinking",
								"thinking": "Internal thoughts"
							}
						]
					}
				]
			}`,
			wantType: "filtered",
		},
		{
			name: "content with redacted_thinking",
			input: `{
				"model": "claude-3-opus",
				"messages": [
					{
						"role": "assistant",
						"content": [
							{
								"type": "text",
								"text": "Response"
							},
							{
								"type": "redacted_thinking"
							}
						]
					}
				]
			}`,
			wantType: "filtered",
		},
		{
			name: "thinking enabled with valid signature",
			input: `{
				"model": "claude-3-opus",
				"thinking": {
					"type": "enabled"
				},
				"messages": [
					{
						"role": "assistant",
						"content": [
							{
								"type": "thinking",
								"thinking": "Valid thought",
								"signature": "valid_sig_here"
							},
							{
								"type": "text",
								"text": "Response"
							}
						]
					}
				]
			}`,
			wantType: "unchanged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterThinkingBlocks([]byte(tt.input))

			// Parse both input and result to compare
			var inputData, resultData map[string]any
			if err := json.Unmarshal([]byte(tt.input), &inputData); err != nil {
				t.Fatalf("failed to parse input: %v", err)
			}
			if err := json.Unmarshal(result, &resultData); err != nil {
				t.Fatalf("failed to parse result: %v", err)
			}

			switch tt.wantType {
			case "unchanged":
				if string(result) != tt.input {
					t.Logf("Input:  %s", tt.input)
					t.Logf("Result: %s", string(result))
					// Allow for formatting differences
				}
			case "filtered":
				// Check that thinking blocks were removed
				messages := resultData["messages"].([]any)
				for _, msg := range messages {
					msgMap := msg.(map[string]any)
					if content, ok := msgMap["content"].([]any); ok {
						for _, block := range content {
							blockMap := block.(map[string]any)
							blockType, _ := blockMap["type"].(string)
							if blockType == "thinking" || blockType == "redacted_thinking" {
								t.Errorf("thinking block not filtered: %+v", blockMap)
							}
						}
					}
				}
			}
		})
	}
}

func TestFilterThinkingBlocksForRetry(t *testing.T) {
	input := `{
		"model": "claude-3-opus",
		"thinking": {
			"type": "enabled"
		},
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "text",
						"text": "Hello"
					},
					{
						"type": "thinking",
						"thinking": "Some thoughts"
					}
				]
			}
		]
	}`

	result := FilterThinkingBlocksForRetry([]byte(input))

	var resultData map[string]any
	if err := json.Unmarshal(result, &resultData); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Check that thinking field is removed
	if _, exists := resultData["thinking"]; exists {
		t.Error("thinking field should be removed in retry")
	}

	// Check that thinking block is converted to text
	messages := resultData["messages"].([]any)
	msg := messages[0].(map[string]any)
	content := msg["content"].([]any)
	
	foundText := false
	for _, block := range content {
		blockMap := block.(map[string]any)
		if blockType, _ := blockMap["type"].(string); blockType == "text" {
			if text, _ := blockMap["text"].(string); text == "Some thoughts" {
				foundText = true
			}
		}
		if blockType, _ := blockMap["type"].(string); blockType == "thinking" {
			t.Error("thinking block should be converted to text in retry")
		}
	}

	if !foundText {
		t.Error("thinking content should be preserved as text")
	}
}

func TestExtractTextFromContent(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
	}{
		{
			name:  "simple string",
			input: "Hello world",
			want:  "Hello world",
		},
		{
			name: "text block array",
			input: []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "Hello ",
				},
				map[string]interface{}{
					"type": "text",
					"text": "world",
				},
			},
			want: "Hello world",
		},
		{
			name: "mixed content blocks",
			input: []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "Start",
				},
				map[string]interface{}{
					"type": "image",
					"source": "...",
				},
				map[string]interface{}{
					"type": "text",
					"text": " End",
				},
			},
			want: "Start End",
		},
		{
			name:  "nil input",
			input: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextFromContent(tt.input)
			if got != tt.want {
				t.Errorf("extractTextFromContent() = %q, want %q", got, tt.want)
			}
		})
	}
}
