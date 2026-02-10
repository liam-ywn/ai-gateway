package providers

import (
	"testing"
)

func TestAnthropicResponse_ToChatResponse(t *testing.T) {
	t.Run("Normal response with content", func(t *testing.T) {
		anthropicResp := &AnthropicResponse{
			ID:   "msg_123",
			Type: "message",
			Role: "assistant",
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: "Hello, world!"},
			},
			Model:      "claude-3-opus-20240229",
			StopReason: "end_turn",
			Usage: AntropicUsage{
				InputTokens:  10,
				OutputTokens: 20,
			},
		}

		chatResp := anthropicResp.ToChatResponse()

		if chatResp.ID != "msg_123" {
			t.Errorf("Expected ID 'msg_123', got '%s'", chatResp.ID)
		}
		if chatResp.Model != "claude-3-opus-20240229" {
			t.Errorf("Expected model 'claude-3-opus-20240229', got '%s'", chatResp.Model)
		}
		if len(chatResp.Choices) != 1 {
			t.Fatalf("Expected 1 choice, got %d", len(chatResp.Choices))
		}
		if chatResp.Choices[0].Message.Content != "Hello, world!" {
			t.Errorf("Expected content 'Hello, world!', got '%s'", chatResp.Choices[0].Message.Content)
		}
		if chatResp.Choices[0].Message.Role != "assistant" {
			t.Errorf("Expected role 'assistant', got '%s'", chatResp.Choices[0].Message.Role)
		}
		if chatResp.Choices[0].FinishReason != "end_turn" {
			t.Errorf("Expected finish reason 'end_turn', got '%s'", chatResp.Choices[0].FinishReason)
		}
		if chatResp.Usage.PromptTokens != 10 {
			t.Errorf("Expected 10 prompt tokens, got %d", chatResp.Usage.PromptTokens)
		}
		if chatResp.Usage.CompletionTokens != 20 {
			t.Errorf("Expected 20 completion tokens, got %d", chatResp.Usage.CompletionTokens)
		}
		if chatResp.Usage.TotalTokens != 30 {
			t.Errorf("Expected 30 total tokens, got %d", chatResp.Usage.TotalTokens)
		}
	})

	t.Run("Empty content array", func(t *testing.T) {
		anthropicResp := &AnthropicResponse{
			ID:   "msg_456",
			Type: "message",
			Role: "assistant",
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{},
			Model:      "claude-3-opus-20240229",
			StopReason: "end_turn",
			Usage: AntropicUsage{
				InputTokens:  5,
				OutputTokens: 0,
			},
		}

		chatResp := anthropicResp.ToChatResponse()

		if chatResp.Choices[0].Message.Content != "" {
			t.Errorf("Expected empty content, got '%s'", chatResp.Choices[0].Message.Content)
		}
	})
}
