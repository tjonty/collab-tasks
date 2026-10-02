// Package config loads the API's settings from the environment.
package config

import "os"

// Config holds the API's runtime settings.
type Config struct {
	Port        string
	DatabaseURL string
	RedisURL    string
	CORSOrigin  string
}

// FromEnv reads the config from the environment. An unset or empty var
// falls back to its local-development default.
func FromEnv() Config {
	return Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://app:app@localhost:5432/tasks?sslmode=disable"),
		RedisURL:    getenv("REDIS_URL", "redis://localhost:6379"),
		CORSOrigin:  getenv("CORS_ORIGIN", "http://localhost:3000"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
