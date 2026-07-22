package feedback

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	Create(ctx context.Context, userID, content string) (Feedback, error)
	List(ctx context.Context, filter ListFilter) (ListResponse, error)
	UpdateStatus(ctx context.Context, id, status string) (Feedback, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Create serializes submissions per user and rejects the same normalized text
// when it was already stored during the last minute. This prevents double-click
// and concurrent retry duplicates without adding an MVP-only idempotency field.
func (s *PostgresStore) Create(ctx context.Context, userID, content string) (Feedback, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Feedback{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, "feedback:"+userID); err != nil {
		return Feedback{}, err
	}

	var existingID string
	err = tx.QueryRow(ctx, `
		select id::text
		from feedbacks
		where user_id = $1::uuid
		  and content = $2
		  and created_at >= now() - interval '60 seconds'
		order by created_at desc
		limit 1
	`, userID, content).Scan(&existingID)
	if err == nil {
		return Feedback{}, ErrDuplicate
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Feedback{}, err
	}

	var item Feedback
	err = tx.QueryRow(ctx, `
		insert into feedbacks (user_id, content)
		values ($1::uuid, $2)
		returning id::text, content, status, created_at
	`, userID, content).Scan(&item.ID, &item.Content, &item.Status, &item.CreatedAt)
	if err != nil {
		return Feedback{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Feedback{}, err
	}
	return item, nil
}

func (s *PostgresStore) List(ctx context.Context, filter ListFilter) (ListResponse, error) {
	response := ListResponse{
		Items:    []Feedback{},
		Page:     filter.Page,
		PageSize: filter.PageSize,
	}
	if err := s.pool.QueryRow(ctx, `
		select count(*)::int
		from feedbacks
		where ($1 = '' or status = $1)
	`, filter.Status).Scan(&response.Total); err != nil {
		return ListResponse{}, err
	}
	if response.Total > 0 {
		response.TotalPages = (response.Total + filter.PageSize - 1) / filter.PageSize
	}
	rows, err := s.pool.Query(ctx, `
		select
			f.id::text,
			u.cn_code,
			coalesce(u.display_name, ''),
			f.content,
			f.status,
			f.created_at
		from feedbacks f
		join users u on u.id = f.user_id
		where ($1 = '' or f.status = $1)
		order by f.created_at desc, f.id desc
		limit $2 offset $3
	`, filter.Status, filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return ListResponse{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var item Feedback
		if err := rows.Scan(
			&item.ID, &item.CNCode, &item.DisplayName, &item.Content,
			&item.Status, &item.CreatedAt,
		); err != nil {
			return ListResponse{}, err
		}
		response.Items = append(response.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ListResponse{}, err
	}
	return response, nil
}

func (s *PostgresStore) UpdateStatus(ctx context.Context, id, status string) (Feedback, error) {
	var item Feedback
	err := s.pool.QueryRow(ctx, `
		with changed as (
			update feedbacks
			set status = $2
			where id = $1::uuid
			returning id, user_id, content, status, created_at
		)
		select
			c.id::text,
			u.cn_code,
			coalesce(u.display_name, ''),
			c.content,
			c.status,
			c.created_at
		from changed c
		join users u on u.id = c.user_id
	`, id, status).Scan(
		&item.ID, &item.CNCode, &item.DisplayName,
		&item.Content, &item.Status, &item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Feedback{}, ErrNotFound
	}
	return item, err
}
