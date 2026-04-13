package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := getEnv("DATABASE_URL", "postgres://nir:nir@localhost:5432/nir?sslmode=disable")
	migrationsDir := getEnv("MIGRATIONS_DIR", "migrations")

	direction := "up"
	if len(os.Args) > 1 {
		direction = os.Args[1]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("DB connection failed: %v", err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create schema_migrations: %v", err)
	}

	suffix := "." + direction + ".sql"
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*"+suffix))
	if err != nil {
		log.Fatalf("Glob error: %v", err)
	}
	if len(files) == 0 {
		log.Printf("No %s migrations found in %s", direction, migrationsDir)
		return
	}

	sort.Strings(files)
	if direction == "down" {
		for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
			files[i], files[j] = files[j], files[i]
		}
	}

	for _, file := range files {
		version := extractVersion(filepath.Base(file))

		if direction == "up" {
			var count int
			_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=$1", version).Scan(&count)
			if count > 0 {
				log.Printf("⏭  Skip %s (already applied)", version)
				continue
			}
		}

		data, err := os.ReadFile(file)
		if err != nil {
			log.Fatalf("Read error: %v", err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Fatalf("Begin tx error: %v", err)
		}

		if _, err := tx.Exec(ctx, string(data)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("❌ Migration %s failed: %v", filepath.Base(file), err)
		}

		if direction == "up" {
			_, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
		} else {
			_, err = tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version=$1", version)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("schema_migrations update error: %v", err)
		}

		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("Commit error: %v", err)
		}
		log.Printf("✅ %s: %s", direction, filepath.Base(file))
	}

	fmt.Println("Done.")
}

func extractVersion(filename string) string {
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return filename
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
