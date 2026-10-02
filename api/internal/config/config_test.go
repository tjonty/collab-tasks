package config

import "testing"

func TestFromEnv_Defaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("CORS_ORIGIN", "")

	got := FromEnv()

	want := Config{
		Port:        "8080",
		DatabaseURL: "postgres://app:app@localhost:5432/tasks?sslmode=disable",
		RedisURL:    "redis://localhost:6379",
		CORSOrigin:  "http://localhost:3000",
	}
	if got != want {
		t.Errorf("FromEnv() = %+v, want %+v", got, want)
	}
}

func TestFromEnv_Overrides(t *testing.T) {
	want := Config{
		Port:        "9090",
		DatabaseURL: "postgres://u:p@db:5432/x?sslmode=disable",
		RedisURL:    "redis://cache:6380",
		CORSOrigin:  "https://app.example.com",
	}
	t.Setenv("PORT", want.Port)
	t.Setenv("DATABASE_URL", want.DatabaseURL)
	t.Setenv("REDIS_URL", want.RedisURL)
	t.Setenv("CORS_ORIGIN", want.CORSOrigin)

	got := FromEnv()

	if got != want {
		t.Errorf("FromEnv() = %+v, want %+v", got, want)
	}
}
