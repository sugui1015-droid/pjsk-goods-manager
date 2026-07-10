package database

import (
	"context"
	"io/fs"
	"log"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrationFS fs.FS, dir string) error {
	if _, err := pool.Exec(ctx, `
		create table if not exists schema_migrations (
			version text primary key,
			applied_at timestamptz not null default now()
		)
	`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationFS, dir)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := migrationApplied(ctx, pool, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, pool, migrationFS, dir, name); err != nil {
			return err
		}
		log.Printf("database migration applied: %s", name)
	}

	return nil
}

func migrationApplied(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	var applied bool
	err := pool.QueryRow(ctx, "select exists(select 1 from schema_migrations where version = $1)", name).Scan(&applied)
	return applied, err
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, migrationFS fs.FS, dir string, name string) error {
	sqlBytes, err := fs.ReadFile(migrationFS, dir+"/"+name)
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(ctx); err != nil {
				log.Printf("rollback migration transaction: %v", err)
			}
		}
	}()

	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "insert into schema_migrations (version) values ($1)", name); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true

	return nil
}
