package paymentqr

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"pjsk/backend/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

type qrFixture struct {
	pool    *pgxpool.Pool
	store   *PostgresStore
	adminID string
	prefix  string
}

func newQRFixture(t *testing.T) qrFixture {
	t.Helper()
	pool := testdb.New(t, "paymentqr")
	f := qrFixture{
		pool:   pool,
		store:  NewPostgresStore(pool),
		prefix: fmt.Sprintf("PAYMENT_QR_TEST_%d", time.Now().UnixNano()),
	}
	f.adminID = f.createAdmin(t, f.prefix+"_admin")
	return f
}

func (f qrFixture) createAdmin(t *testing.T, username string) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(), `
		insert into admins (username, password_hash, status)
		values ($1, 'test-hash', 'active')
		returning id::text
	`, username).Scan(&id); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	return id
}

func (f qrFixture) countRows(t *testing.T, method string) (total, enabled int) {
	t.Helper()
	if err := f.pool.QueryRow(context.Background(), `
		select
			count(*),
			count(*) filter (where enabled)
		from payment_qr_codes
		where payment_method = $1
	`, method).Scan(&total, &enabled); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return total, enabled
}

func img(mime, sha string, size int) StoredImage {
	return StoredImage{Data: []byte("fake-bytes-" + sha), MimeType: mime, SHA256: sha, ByteSize: size}
}

func sha(n byte) string {
	out := make([]byte, 64)
	for i := range out {
		out[i] = "0123456789abcdef"[int(n)%16]
	}
	return string(out)
}

func TestUploadReplaceKeepsSingleActiveAndHistory(t *testing.T) {
	f := newQRFixture(t)
	ctx := context.Background()

	if err := f.store.Upload(ctx, MethodAlipay, img("image/png", sha(1), 100), f.adminID); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	total, enabled := f.countRows(t, MethodAlipay)
	if total != 1 || enabled != 1 {
		t.Fatalf("after first upload total=%d enabled=%d, want 1/1", total, enabled)
	}

	// Replace: must leave exactly one enabled row and retain the old as history.
	if err := f.store.Upload(ctx, MethodAlipay, img("image/jpeg", sha(2), 200), f.adminID); err != nil {
		t.Fatalf("replace upload: %v", err)
	}
	total, enabled = f.countRows(t, MethodAlipay)
	if total != 2 || enabled != 1 {
		t.Fatalf("after replace total=%d enabled=%d, want 2/1", total, enabled)
	}

	active, err := f.store.ActiveImage(ctx, MethodAlipay)
	if err != nil {
		t.Fatalf("active image: %v", err)
	}
	if active.MimeType != "image/jpeg" || active.SHA256 != sha(2) {
		t.Fatalf("active image = %s/%s, want jpeg/%s", active.MimeType, active.SHA256, sha(2))
	}
}

func TestDisableRemovesActiveButKeepsHistory(t *testing.T) {
	f := newQRFixture(t)
	ctx := context.Background()

	if err := f.store.Upload(ctx, MethodWechat, img("image/png", sha(3), 100), f.adminID); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := f.store.Disable(ctx, MethodWechat, f.adminID); err != nil {
		t.Fatalf("disable: %v", err)
	}

	total, enabled := f.countRows(t, MethodWechat)
	if total != 1 || enabled != 0 {
		t.Fatalf("after disable total=%d enabled=%d, want 1/0 (history retained, none active)", total, enabled)
	}
	if _, err := f.store.ActiveImage(ctx, MethodWechat); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("ActiveImage after disable = %v, want ErrNotConfigured", err)
	}

	// Disabling again with nothing active returns ErrNotConfigured.
	if err := f.store.Disable(ctx, MethodWechat, f.adminID); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("second disable = %v, want ErrNotConfigured", err)
	}
}

func TestConcurrentUploadsYieldSingleActive(t *testing.T) {
	f := newQRFixture(t)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = f.store.Upload(ctx, MethodAlipay, img("image/png", sha(byte(i+1)), 100+i), f.adminID)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent upload %d error: %v", i, err)
		}
	}
	total, enabled := f.countRows(t, MethodAlipay)
	if enabled != 1 {
		t.Fatalf("enabled rows = %d, want exactly 1 after concurrent uploads", enabled)
	}
	if total != n {
		t.Fatalf("total rows = %d, want %d (every upload retained)", total, n)
	}
}

func TestAdminStatusesAndUserAvailability(t *testing.T) {
	f := newQRFixture(t)
	ctx := context.Background()

	if err := f.store.Upload(ctx, MethodAlipay, img("image/png", sha(5), 123), f.adminID); err != nil {
		t.Fatalf("upload: %v", err)
	}

	statuses, err := f.store.AdminStatuses(ctx)
	if err != nil {
		t.Fatalf("admin statuses: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %d, want 2", len(statuses))
	}
	var alipay, wechat AdminStatus
	for _, st := range statuses {
		switch st.PaymentMethod {
		case MethodAlipay:
			alipay = st
		case MethodWechat:
			wechat = st
		}
	}
	if !alipay.Configured || alipay.MimeType != "image/png" || alipay.ByteSize != 123 || alipay.UpdatedBy != f.prefix+"_admin" {
		t.Fatalf("alipay status = %#v", alipay)
	}
	if wechat.Configured {
		t.Fatalf("wechat should be unconfigured: %#v", wechat)
	}

	avail, err := f.store.UserAvailability(ctx)
	if err != nil {
		t.Fatalf("availability: %v", err)
	}
	got := map[string]bool{}
	for _, a := range avail {
		got[a.PaymentMethod] = a.Available
	}
	if !got[MethodAlipay] || got[MethodWechat] {
		t.Fatalf("availability = %#v, want alipay true wechat false", got)
	}
}
