package config

import (
	"os"
	"strings"
)

type Config struct {
	ServerAddress string
	WebAddress    string
	GRPCAddress   string
	DatabaseURL   string
	Debug         bool
}

func Load() Config {
	return Config{
		ServerAddress: getEnv("SERVER_ADDRESS", ":50051"),
		WebAddress:    getEnv("WEB_ADDRESS", ":8080"),
		GRPCAddress:   getEnv("GRPC_ADDRESS", "localhost:50051"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://nir:nir@localhost:5432/nir?sslmode=disable"),
		Debug:         strings.ToLower(getEnv("LOG_LEVEL", "info")) == "debug",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
