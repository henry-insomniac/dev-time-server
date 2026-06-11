package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/db"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestRepositoryImportIsIdempotentAfterMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	repository, err := store.UpsertRepository(ctx, db.RepositoryInput{
		GitHubID: 1001,
		Owner:    "henry-insomniac",
		Name:     "dev-time",
		FullName: "henry-insomniac/dev-time",
	})
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}

	sameRepository, err := store.UpsertRepository(ctx, db.RepositoryInput{
		GitHubID: 1001,
		Owner:    "henry-insomniac",
		Name:     "dev-time",
		FullName: "henry-insomniac/dev-time",
	})
	if err != nil {
		t.Fatalf("upsert same repository: %v", err)
	}

	if sameRepository.ID != repository.ID {
		t.Fatalf("expected idempotent repository id %q, got %q", repository.ID, sameRepository.ID)
	}

	project, err := store.EnsureProjectForRepository(ctx, repository.ID, "Dev Time")
	if err != nil {
		t.Fatalf("ensure project: %v", err)
	}

	sameProject, err := store.EnsureProjectForRepository(ctx, repository.ID, "Dev Time")
	if err != nil {
		t.Fatalf("ensure same project: %v", err)
	}

	if sameProject.ID != project.ID {
		t.Fatalf("expected idempotent project id %q, got %q", project.ID, sameProject.ID)
	}
}
