package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}

	if p.apiKey == "mock" {
		return &providers.ChatResponse{
			ID:      "mock-openai-id",
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
						Content: fmt.Sprintf("OpenAI Mock: %s", req.Messages[len(req.Messages)-1].Content),
					},
					FinishReason: "stop",
				},
			},
			Usage: providers.Usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		}, nil
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", p.baseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errData)
		return nil, fmt.Errorf("openai error (status %d): %v", resp.StatusCode, errData)
	}

	var chatResp providers.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, err
	}

	return &chatResp, nil
}

func (p *Provider) ChatStream(req providers.ChatRequest) (<-chan providers.ChatChunk, <-chan error) {
	chunkCh := make(chan providers.ChatChunk)
	errCh := make(chan error, 1)

	if p.apiKey == "mock" {
		go func() {
			defer close(chunkCh)
			defer close(errCh)
			content := fmt.Sprintf("OpenAI Mock Stream: %s", req.Messages[len(req.Messages)-1].Content)
			words := strings.Split(content, " ")
			for i, word := range words {
				chunkCh <- providers.ChatChunk{
					ID:      "mock-stream-id",
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
						ID:      "mock-stream-id",
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

		httpReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(body))
		if err != nil {
			errCh <- err
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errData map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&errData)
			errCh <- fmt.Errorf("openai streaming error (status %d): %v", resp.StatusCode, errData)
			return
		}

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk providers.ChatChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			chunkCh <- chunk
		}
	}()

	return chunkCh, errCh
}
