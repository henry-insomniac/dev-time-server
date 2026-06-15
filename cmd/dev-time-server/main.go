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

	if err := db.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		if !loaded.AllowNoDatabase {
			log.Fatalf("run database migrations: %v", err)
		}
		log.Printf("database unavailable; starting without persistent store: %v", err)
		pool = nil
	}

	log.Printf("starting %s on %s", buildinfo.ServiceName(), loaded.ServerAddr)
	var store *db.Store
	if pool != nil {
		defer pool.Close()
		store = db.NewStore(pool)
	}

	if err := http.ListenAndServe(
		loaded.ServerAddr,
		api.NewRouter(api.Dependencies{
			Store:               store,
			AgentRuntimeBaseURL: loaded.AgentRuntimeBaseURL,
			GitHubApp: api.GitHubAppConfig{
				AppID:               loaded.GitHubAppID,
				AppSlug:             loaded.GitHubAppSlug,
				PrivateKeyPath:      loaded.GitHubPrivateKeyPath,
				SetupStateSecret:    loaded.GitHubSetupStateSecret,
				APIBaseURL:          loaded.GitHubAPIBaseURL,
				InstallationBaseURL: loaded.GitHubInstallBaseURL,
				FrontendBaseURL:     loaded.FrontendBaseURL,
			},
		}),
	); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
