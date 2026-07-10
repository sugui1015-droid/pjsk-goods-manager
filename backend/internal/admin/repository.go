package admin

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("admin session or account not found")

type Admin struct {
	ID           string
	Username     string
	PasswordHash string
	DisplayName  *string
	Role         string
	Status       string
}

type Store interface {
	FindByUsername(context.Context, string) (Admin, error)
	CreateSession(context.Context, string, string, time.Time) error
	FindBySession(context.Context, string) (Admin, error)
	DeleteSession(context.Context, string) error
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) FindByUsername(ctx context.Context, username string) (Admin, error) {
	var account Admin
	err := s.pool.QueryRow(ctx, `
		select id::text, username, password_hash, display_name, role, status
		from admins
		where lower(btrim(username)) = lower($1)
	`, username).Scan(
		&account.ID,
		&account.Username,
		&account.PasswordHash,
		&account.DisplayName,
		&account.Role,
		&account.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	return account, err
}

func (s *PostgresStore) CreateSession(
	ctx context.Context,
	adminID string,
	tokenHash string,
	expiresAt time.Time,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into admin_sessions (admin_id, token_hash, expires_at)
		values ($1::uuid, $2, $3)
	`, adminID, tokenHash, expiresAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update admins set last_login_at = now(), updated_at = now()
		where id = $1::uuid
	`, adminID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) FindBySession(ctx context.Context, tokenHash string) (Admin, error) {
	var account Admin
	err := s.pool.QueryRow(ctx, `
		with valid_session as (
			update admin_sessions
			set last_used_at = now()
			where token_hash = $1 and expires_at > now()
			returning admin_id
		)
		select a.id::text, a.username, a.password_hash, a.display_name, a.role, a.status
		from valid_session s
		join admins a on a.id = s.admin_id
		where a.status = 'active'
	`, tokenHash).Scan(
		&account.ID,
		&account.Username,
		&account.PasswordHash,
		&account.DisplayName,
		&account.Role,
		&account.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	return account, err
}

func (s *PostgresStore) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, "delete from admin_sessions where token_hash = $1", tokenHash)
	return err
}
