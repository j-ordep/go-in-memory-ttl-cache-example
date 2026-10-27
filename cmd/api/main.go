package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"turnover-api/internal/analytics"
	"turnover-api/internal/httpapi"
)

func main() {
	ctx := context.Background()

	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("conectando ao banco", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	cat := analytics.NewCatalog(db, time.Minute)
	repo := analytics.NewRepo(db, cat)
	srv := httpapi.NewServer(repo, cat)

	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}
	slog.Info("API ouvindo", "addr", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		slog.Error("servidor parou", "err", err)
		os.Exit(1)
	}
}
