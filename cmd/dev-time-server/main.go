package main

import (
	"context"
	"log"
	"net/http"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/buildinfo"
	"github.com/henry-insomniac/dev-time-server/internal/config"
	"github.com/henry-insomniac/dev-time-server/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()
	loaded := config.Load()

	pool, err := pgxpool.New(ctx, loaded.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("run database migrations: %v", err)
	}

	log.Printf("starting %s on %s", buildinfo.ServiceName(), loaded.ServerAddr)

	if err := http.ListenAndServe(
		loaded.ServerAddr,
		api.NewRouter(api.Dependencies{
			Store:               db.NewStore(pool),
			AgentRuntimeBaseURL: loaded.AgentRuntimeBaseURL,
		}),
	); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
