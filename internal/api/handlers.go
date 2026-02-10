package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/yewintnaing/ai-gateway/internal/config"
	"github.com/yewintnaing/ai-gateway/internal/providers"
	"github.com/yewintnaing/ai-gateway/internal/ratelimit"
	"github.com/yewintnaing/ai-gateway/internal/router"
	"github.com/yewintnaing/ai-gateway/internal/usage"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Handler struct {
	router   *router.Router
	registry providers.Registry
	usage    *usage.Store
	limiter  *ratelimit.Limiter
	tracer   trace.Tracer
}

func NewHandler(r *router.Router, reg providers.Registry, s *usage.Store, l *ratelimit.Limiter) *Handler {
	return &Handler{
		router:   r,
		registry: reg,
		usage:    s,
		limiter:  l,
		tracer:   otel.Tracer("gateway-handler"),
	}
}

type ChatRequest struct {
	Model       string                 `json:"model"`
	Messages    []providers.Message    `json:"messages"`
	Temperature float64                `json:"temperature"`
	MaxTokens   int                    `json:"max_tokens"`
	Stream      bool                   `json:"stream"`
	Metadata    map[string]interface{} `json:"metadata"`
}

func (h *Handler) HandleChat(w http.ResponseWriter, r *http.Request) {
	ctx, span := h.tracer.Start(r.Context(), "HandleChat")
	defer span.End()

	start := time.Now()
	requestID := r.Header.Get("x-request-id")
	if requestID == "" {
		requestID = uuid.New().String()
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body", requestID)
		return
	}

	tenant, _ := req.Metadata["tenant"].(string)
	if tenant == "" {
		tenant = "anonymous"
	}
	useCase, _ := req.Metadata["use_case"].(string)

	span.SetAttributes(
		attribute.String("request_id", requestID),
		attribute.String("tenant", tenant),
		attribute.String("use_case", useCase),
	)

	// Rate Limiting
	caller := tenant // Simplification: use tenant as caller
	promptTokens := usage.ApproximateTokens(fmt.Sprintf("%v", req.Messages))
	allowed, err := h.limiter.Allow(ctx, caller, promptTokens)
	if err != nil {
		logError(requestID, "rate limit check failed", err)
	}
	if !allowed {
		h.usage.Log(ctx, usage.Record{RequestID: requestID, Tenant: tenant, UseCase: useCase, StatusCode: http.StatusTooManyRequests, ErrorMessage: "rate limited"})
		h.respondError(w, http.StatusTooManyRequests, "rate limited", requestID)
		return
	}

	// Routing
	route := h.router.Route(useCase)
	span.SetAttributes(attribute.String("route_name", route.Name))

	// Ensure request row exists for attempts
	h.usage.Log(ctx, usage.Record{
		RequestID: requestID,
		Tenant:    tenant,
		UseCase:   useCase,
		RouteName: route.Name,
	})

	// Attempt coordination
	var lastErr error

	targets := append([]config.Target{route.Primary}, route.Fallbacks...)
	attemptNo := 1

	for _, target := range targets {
		for i := 0; i <= route.Retries; i++ {
			tCtx, tSpan := h.tracer.Start(ctx, "ProviderAttempt", trace.WithAttributes(
				attribute.String("provider", target.Provider),
				attribute.String("model", target.Model),
				attribute.Int("attempt_no", attemptNo),
			))

			provider, pErr := h.registry.Get(target.Provider)
			if pErr != nil {
				tSpan.End()
				lastErr = pErr
				break
			}

			provReq := providers.ChatRequest{
				Model:       target.Model,
				Messages:    req.Messages,
				Temperature: req.Temperature,
				MaxTokens:   req.MaxTokens,
				Stream:      req.Stream,
			}

			attemptStart := time.Now()

			if req.Stream {
				h.handleStream(tCtx, w, r, provider, provReq, requestID, route, target, tenant, useCase, attemptNo)
				tSpan.End()
				return // handleStream takes over the response
			}

			resp, err := provider.Chat(provReq)
			latency := int(time.Since(attemptStart).Milliseconds())

			h.usage.LogAttempt(tCtx, requestID, usage.Attempt{
				RequestID:    requestID,
				AttemptNo:    attemptNo,
				Provider:     target.Provider,
				Model:        target.Model,
				LatencyMS:    latency,
				StatusCode:   getStatusCode(err, resp != nil),
				ErrorMessage: getErrorMessage(err),
			})

			if err == nil {
				h.usage.Log(tCtx, usage.Record{
					RequestID: requestID, Tenant: tenant, UseCase: useCase, RouteName: route.Name,
					Provider: target.Provider, Model: target.Model,
					PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens, TotalTokens: resp.Usage.TotalTokens,
					LatencyMS: int(time.Since(start).Milliseconds()), StatusCode: http.StatusOK,
				})

				w.Header().Set("x-request-id", requestID)
				w.Header().Set("x-gw-route", route.Name)
				w.Header().Set("x-gw-provider", target.Provider)
				w.Header().Set("x-gw-model", target.Model)
				json.NewEncoder(w).Encode(resp)
				tSpan.End()
				return
			}

			tSpan.RecordError(err)
			tSpan.End()
			lastErr = err
			attemptNo++

			if !router.IsRetryable(err) {
				logError(requestID, "non-retryable error", err)
				break
			}
		}
	}
	h.respondError(w, http.StatusBadGateway, lastErr.Error(), requestID)
}

func (h *Handler) handleStream(ctx context.Context, w http.ResponseWriter, r *http.Request, p providers.Provider, req providers.ChatRequest, requestID string, route config.Route, target config.Target, tenant, useCase string, attemptNo int) {
	chunkCh, errCh := p.ChatStream(req)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("x-request-id", requestID)
	w.Header().Set("x-gw-route", route.Name)
	w.Header().Set("x-gw-provider", target.Provider)
	w.Header().Set("x-gw-model", target.Model)

	flusher, _ := w.(http.Flusher)

	fullContent := ""
	start := time.Now()

	for {
		select {
		case chunk, ok := <-chunkCh:
			if !ok {
				// Log final success record for stream
				h.usage.Log(r.Context(), usage.Record{
					RequestID: requestID, Tenant: tenant, UseCase: useCase, RouteName: route.Name,
					Provider: target.Provider, Model: target.Model,
					PromptTokens:     usage.ApproximateTokens(fmt.Sprintf("%v", req.Messages)),
					CompletionTokens: usage.ApproximateTokens(fullContent),
					TotalTokens:      usage.ApproximateTokens(fmt.Sprintf("%v", req.Messages)) + usage.ApproximateTokens(fullContent),
					LatencyMS:        int(time.Since(start).Milliseconds()), StatusCode: http.StatusOK,
				})
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			if len(chunk.Choices) > 0 {
				fullContent += chunk.Choices[0].Delta.Content
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		case err := <-errCh:
			if err != nil {
				h.usage.LogAttempt(r.Context(), requestID, usage.Attempt{
					RequestID: requestID, AttemptNo: attemptNo, Provider: target.Provider, Model: target.Model,
					StatusCode: http.StatusBadGateway, ErrorMessage: err.Error(),
				})
				// Mid-stream error handling: send error event
				fmt.Fprintf(w, "data: {\"error\": {\"message\": %q}}\n\n", err.Error())
				flusher.Flush()
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handler) respondError(w http.ResponseWriter, code int, msg string, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-request-id", requestID)
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{"message": msg, "request_id": requestID},
	})
}

func getStatusCode(err error, success bool) int {
	if success {
		return http.StatusOK
	}
	return http.StatusBadGateway
}

func getErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func logError(requestID, msg string, err error) {
	println(fmt.Sprintf("[%s] %s: %v", requestID, msg, err))
}
