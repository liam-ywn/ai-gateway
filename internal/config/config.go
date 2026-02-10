package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port             string
	DatabaseURL      string
	OpenAIKey        string
	OpenAIURL        string
	OpenAIVersion    string
	AnthropicKey     string
	AnthropicURL     string
	AnthropicVersion string
	RedisURL         string
	TPM              int
	Routes           []Route
}

type Target struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

type Route struct {
	Name      string   `yaml:"name"`
	Match     Match    `yaml:"match"`
	Primary   Target   `yaml:"primary"`
	Fallbacks []Target `yaml:"fallbacks"`
	TimeoutMS int      `yaml:"timeout_ms"`
	Retries   int      `yaml:"retries"`
}

type Match struct {
	UseCase string `yaml:"use_case"`
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:             getEnv("PORT", "8080"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/aigw?sslmode=disable"),
		OpenAIKey:        os.Getenv("OPENAI_API_KEY"),
		OpenAIURL:        getEnv("OPENAI_API_URL", "https://api.openai.com/v1"),
		OpenAIVersion:    getEnv("OPENAI_API_VERSION", "v1"),
		AnthropicKey:     os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicURL:     getEnv("ANTHROPIC_API_URL", "https://api.anthropic.com/v1"),
		AnthropicVersion: getEnv("ANTHROPIC_API_VERSION", "2023-06-01"),
		RedisURL:         getEnv("REDIS_URL", "redis://localhost:6379/0"),
		TPM:              getTPM(),
	}

	routesPath := getEnv("ROUTES_CONFIG", "configs/routes.yaml")
	routes, err := loadRoutes(routesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load routes: %w", err)
	}
	cfg.Routes = routes

	return cfg, nil
}

func loadRoutes(path string) ([]Route, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var wrapper struct {
		Routes []Route `yaml:"routes"`
	}
	if err := yaml.NewDecoder(f).Decode(&wrapper); err != nil {
		return nil, err
	}
	return wrapper.Routes, nil
}

func getTPM() int {
	val := getEnv("TOKENS_PER_MINUTE", "50000")
	var tpm int
	fmt.Sscanf(val, "%d", &tpm)
	return tpm
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
