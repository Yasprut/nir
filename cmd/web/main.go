package main

import (
	"log"
	"net/http"

	"nir/internal/config"
	"nir/internal/storage"
	"nir/internal/web"
	pb "nir/proto/iam/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	cfg := config.Load()

	store, err := storage.NewPostgresStore(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()

	// Соединение с gRPC-сервером — ленивое, ошибка будет при первом запросе
	conn, err := grpc.NewClient(cfg.GRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("grpc dial: %v", err)
	}
	defer conn.Close()

	grpcClient := pb.NewIAMClient(conn)
	srv := web.NewServer(store, grpcClient)

	log.Printf("Веб-интерфейс запущен на %s (gRPC → %s)", cfg.WebAddress, cfg.GRPCAddress)
	if err := http.ListenAndServe(cfg.WebAddress, srv); err != nil {
		log.Fatalf("http server: %v", err)
	}
}
