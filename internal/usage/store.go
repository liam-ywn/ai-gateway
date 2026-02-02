package usage

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Record struct {
	RequestID        string
	Tenant           string
	UseCase          string
	RouteName        string
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostEstimate     float64
	LatencyMS        int
	StatusCode       int
	ErrorMessage     string
}

type Attempt struct {
	RequestID    string
	AttemptNo    int
	Provider     string
	Model        string
	LatencyMS    int
	StatusCode   int
	ErrorMessage string
}

type Store struct {
	db *pgxpool.Pool
}

func NewStore(connString string) (*Store, error) {
	db, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Log(ctx context.Context, r Record) error {
	cost := EstimateCost(r.Model, r.PromptTokens, r.CompletionTokens)

	_, err := s.db.Exec(ctx, `
		INSERT INTO requests (request_id, tenant, use_case, route_name, provider, model, prompt_tokens, completion_tokens, total_tokens, cost_estimate_usd, latency_ms, status_code, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (request_id) DO UPDATE SET
			tenant = EXCLUDED.tenant,
			use_case = EXCLUDED.use_case,
			route_name = EXCLUDED.route_name,
			provider = EXCLUDED.provider,
			model = EXCLUDED.model,
			prompt_tokens = EXCLUDED.prompt_tokens,
			completion_tokens = EXCLUDED.completion_tokens,
			total_tokens = EXCLUDED.total_tokens,
			cost_estimate_usd = EXCLUDED.cost_estimate_usd,
			latency_ms = EXCLUDED.latency_ms,
			status_code = EXCLUDED.status_code,
			error_message = EXCLUDED.error_message
	`, r.RequestID, r.Tenant, r.UseCase, r.RouteName, r.Provider, r.Model, r.PromptTokens, r.CompletionTokens, r.TotalTokens, cost, r.LatencyMS, r.StatusCode, r.ErrorMessage)
	return err
}

func (s *Store) LogAttempt(ctx context.Context, reqCorrelationID string, a Attempt) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO provider_attempts (request_id, attempt_no, provider, model, latency_ms, status_code, error_message)
		SELECT id, $2, $3, $4, $5, $6, $7 FROM requests WHERE request_id = $1 LIMIT 1
	`, reqCorrelationID, a.AttemptNo, a.Provider, a.Model, a.LatencyMS, a.StatusCode, a.ErrorMessage)
	return err
}

func (s *Store) Close() {
	s.db.Close()
}

// EstimateCost calculates approximate cost based on provided pricing
func EstimateCost(model string, promptTokens, completionTokens int) float64 {
	var inputRate, outputRate float64 // per 1M tokens

	switch model {
	case "gpt-4o-mini":
		inputRate = 0.15
		outputRate = 0.60
	case "gpt-4.1-mini": // Placeholder for future models
		inputRate = 0.30
		outputRate = 1.20
	case "claude-3-5-sonnet":
		inputRate = 3.00
		outputRate = 15.00
	default:
		inputRate = 0.15
		outputRate = 0.60
	}

	cost := (float64(promptTokens) / 1000000.0 * inputRate) + (float64(completionTokens) / 1000000.0 * outputRate)
	return math.Round(cost*1000000) / 1000000 // Round to 6 decimal places
}

// ApproximateTokens is useful for cases where usage isn't returned (e.g. errors before downstream call)
func ApproximateTokens(text string) int {
	return len(text) / 4
}
