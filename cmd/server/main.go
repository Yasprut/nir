package main

import (
	"context"
	"log"
	"net"

	"nir/internal/config"
	"nir/internal/handler"
	"nir/internal/policy"
	"nir/internal/storage"
	pb "nir/proto/iam/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg := config.Load()

	store, err := storage.NewPostgresStore(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer store.Close()

	policies, err := store.LoadPolicies(context.Background())
	if err != nil {
		log.Fatalf("failed to load policies: %v", err)
	}
	log.Printf("Loaded %d policies from PostgreSQL", len(policies))

	engine, err := policy.NewEngine(policies)
	if err != nil {
		log.Fatalf("failed to create policy engine: %v", err)
	}

	holder := policy.NewEngineHolder(engine)
	h := handler.NewIAMHandler(holder, cfg.Debug)

	lis, err := net.Listen("tcp", cfg.ServerAddress)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterIAMServer(grpcServer, h)
	reflection.Register(grpcServer)

	logLevel := "info"
	if cfg.Debug {
		logLevel = "debug"
	}
	log.Printf("IAM Routing запущен на %s (log_level=%s)", cfg.ServerAddress, logLevel)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
