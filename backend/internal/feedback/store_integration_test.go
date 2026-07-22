package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/testdb"
)

func TestFeedbackStoreLifecycleIntegration(t *testing.T) {
	pool := testdb.New(t, "feedback")
	store := NewPostgresStore(pool)
	cn := fmt.Sprintf("FEEDBACK_TEST_%d", time.Now().UnixNano())
	var userID string
	if err := pool.QueryRow(context.Background(), `
		insert into users (cn_code, display_name, status)
		values ($1, 'feedback test user', 'active')
		returning id::text
	`, cn).Scan(&userID); err != nil {
		t.Fatalf("create feedback test user: %v", err)
	}

	created, err := store.Create(context.Background(), userID, "希望增加筛选")
	if err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	if created.Status != StatusNew || created.Content != "希望增加筛选" {
		t.Fatalf("created = %#v", created)
	}
	if _, err := store.Create(context.Background(), userID, "希望增加筛选"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate error = %v, want ErrDuplicate", err)
	}

	listed, err := store.List(context.Background(), ListFilter{Status: StatusNew, Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].CNCode != cn {
		t.Fatalf("listed = %#v", listed)
	}

	updated, err := store.UpdateStatus(context.Background(), created.ID, StatusProcessed)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if updated.Status != StatusProcessed || updated.ID != created.ID {
		t.Fatalf("updated = %#v", updated)
	}

	for name, content := range map[string]string{
		"empty":    "   ",
		"too_long": strings.Repeat("意", 1001),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(context.Background(), `insert into feedbacks (user_id, content) values ($1::uuid, $2)`, userID, content); err == nil {
				t.Fatal("database accepted invalid feedback content")
			}
		})
	}
}
