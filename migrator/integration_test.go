package migrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationUpRollbackReset(t *testing.T) {
	databaseURL := os.Getenv("PGXMIGRATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PGXMIGRATE_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig admin: %v", err)
	}
	defer admin.Close()

	schema := fmt.Sprintf("pgxmigrate_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)

	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer pool.Close()

	dir := t.TempDir()
	writeMigration(t, dir, "20250101000000_create_widgets.sql", `
----------UP----------
CREATE TABLE widgets (
	id bigserial PRIMARY KEY,
	name text NOT NULL
);
----------DOWN----------
DROP TABLE widgets;
`)
	writeMigration(t, dir, "20250101000001_create_widget_events.sql", `
----------UP----------
CREATE TABLE widget_events (
	id bigserial PRIMARY KEY,
	widget_id bigint NOT NULL REFERENCES widgets(id)
);
----------DOWN----------
DROP TABLE widget_events;
`)

	m := New(pool, dir)
	if err := m.Ensure(ctx); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	applied, err := m.Up(ctx, 0)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied migrations, got %d", len(applied))
	}

	rolledBack, err := m.Rollback(ctx)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if len(rolledBack) != 2 {
		t.Fatalf("expected 2 rolled back migrations, got %d", len(rolledBack))
	}

	applied, err = m.Up(ctx, 1)
	if err != nil {
		t.Fatalf("Up one: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied migration, got %d", len(applied))
	}

	reset, err := m.Reset(ctx)
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if len(reset) != 1 {
		t.Fatalf("expected 1 reset migration, got %d", len(reset))
	}
}

func writeMigration(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
		t.Fatalf("write migration: %v", err)
	}
}
