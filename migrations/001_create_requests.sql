CREATE TABLE IF NOT EXISTS requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id TEXT NOT NULL UNIQUE,
    tenant TEXT,
    use_case TEXT,
    route_name TEXT,
    provider TEXT,
    model TEXT,
    prompt_tokens INT,
    completion_tokens INT,
    total_tokens INT,
    cost_estimate_usd NUMERIC(12,6),
    latency_ms INT,
    status_code INT,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
