package anthropic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yewintnaing/ai-gateway/internal/providers"
)

type Provider struct {
	apiKey  string
	baseURL string
	version string
	client  *http.Client
}

func NewProvider(apiKey string, baseURL string, version string) *Provider {
	return &Provider{
		apiKey:  apiKey,
		baseURL: baseURL,
		version: version,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) Chat(req providers.ChatRequest) (*providers.ChatResponse, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

	if p.apiKey == "mock" {
		return &providers.ChatResponse{
			ID:      "mock-anthropic-id",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []struct {
				Index        int               `json:"index"`
				Message      providers.Message `json:"message"`
				FinishReason string            `json:"finish_reason"`
			}{
				{
					Index: 0,
					Message: providers.Message{
						Role:    "assistant",
						Content: fmt.Sprintf("Anthropic Mock: %s", req.Messages[len(req.Messages)-1].Content),
					},
					FinishReason: "stop",
				},
			},
			Usage: providers.Usage{
				PromptTokens:     15,
				CompletionTokens: 25,
				TotalTokens:      40,
			},
		}, nil
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", p.baseURL+"/messages", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", p.apiKey)
	httpReq.Header.Set("Anthropic-Version", p.version)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResponse providers.AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResponse); err != nil {
		return nil, err
	}

	return chatResponse.ToChatResponse(), nil

}

func (p *Provider) ChatStream(req providers.ChatRequest) (<-chan providers.ChatChunk, <-chan error) {
	chunkCh := make(chan providers.ChatChunk)
	errCh := make(chan error, 1)

	if p.apiKey == "mock" {
		go func() {
			defer close(chunkCh)
			defer close(errCh)
			content := fmt.Sprintf("Anthropic Mock Stream: %s", req.Messages[len(req.Messages)-1].Content)
			words := strings.Split(content, " ")
			for i, word := range words {
				chunkCh <- providers.ChatChunk{
					ID:      "mock-anth-stream-id",
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   req.Model,
					Choices: []struct {
						Index int `json:"index"`
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
						FinishReason string `json:"finish_reason"`
					}{
						{
							Index: 0,
							Delta: struct {
								Content string `json:"content"`
							}{Content: word + " "},
						},
					},
				}
				if i == len(words)-1 {
					chunkCh <- providers.ChatChunk{
						ID:      "mock-anth-stream-id",
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   req.Model,
						Choices: []struct {
							Index int `json:"index"`
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
							FinishReason string `json:"finish_reason"`
						}{
							{
								Index:        0,
								FinishReason: "stop",
							},
						},
					}
				}
				time.Sleep(50 * time.Millisecond)
			}
		}()
		return chunkCh, errCh
	}

	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		errCh <- err
		return chunkCh, errCh
	}

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		httpReq, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(body))
		if err != nil {
			errCh <- err
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-API-Key", p.apiKey)
		httpReq.Header.Set("Anthropic-Version", p.version)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errData map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errData)
			errCh <- fmt.Errorf("anthropic streaming error (status %d): %v", resp.StatusCode, errData)
			return
		}

		// Parse SSE stream
		reader := bufio.NewReader(resp.Body)
		var messageID string
		var model string
		var created int64

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Parse event type
			var eventType string
			var eventData string

			if after, ok := strings.CutPrefix(line, "event: "); ok {
				eventType = after
				// Read the next line for data
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				if after, ok := strings.CutPrefix(dataLine, "data: "); ok {
					eventData = after
				}
			} else {
				continue
			}

			// Handle different event types
			switch eventType {
			case "message_start":
				var msgStart providers.AnthropicMessageStart
				if err := json.Unmarshal([]byte(eventData), &msgStart); err != nil {
					continue
				}
				messageID = msgStart.Message.ID
				model = msgStart.Message.Model
				created = time.Now().Unix()

			case "content_block_delta":
				var delta providers.AnthropicContentBlockDelta
				if err := json.Unmarshal([]byte(eventData), &delta); err != nil {
					continue
				}

				// Only process text deltas
				if delta.Delta.Type == "text_delta" {
					chunkCh <- providers.ChatChunk{
						ID:      messageID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []struct {
							Index int `json:"index"`
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
							FinishReason string `json:"finish_reason"`
						}{
							{
								Index: delta.Index,
								Delta: struct {
									Content string `json:"content"`
								}{Content: delta.Delta.Text},
							},
						},
					}
				}

			case "message_delta":
				var msgDelta providers.AnthropicMessageDelta
				if err := json.Unmarshal([]byte(eventData), &msgDelta); err != nil {
					continue
				}

				// Send final chunk with finish_reason
				if msgDelta.Delta.StopReason != "" {
					chunkCh <- providers.ChatChunk{
						ID:      messageID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []struct {
							Index int `json:"index"`
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
							FinishReason string `json:"finish_reason"`
						}{
							{
								Index:        0,
								FinishReason: msgDelta.Delta.StopReason,
							},
						},
					}
				}

			case "message_stop":
				// Stream complete
				return

			case "ping":
				// Ignore ping events
				continue

			case "error":
				// Handle error events
				var errEvent map[string]interface{}
				if err := json.Unmarshal([]byte(eventData), &errEvent); err == nil {
					errCh <- fmt.Errorf("anthropic stream error: %v", errEvent)
				}
				return
			}
		}
	}()

	return chunkCh, errCh
}
