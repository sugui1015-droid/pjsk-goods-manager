package export

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/payments"
	"pjsk/backend/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestOrderItemsCSVFiltersByIndependentRoleAndCategory confirms 谷子种类
// (category) and 谷子角色 (role) act as independent AND-combined filters —
// not folded into a single "search everything" field — and that the export
// endpoint honors the same params the on-screen table filter would send, so
// exported rows always match what was actually filtered on screen.
func TestOrderItemsCSVFiltersByIndependentRoleAndCategory(t *testing.T) {
	pool := newExportTestPool(t)
	prefix := "EXPORT_FILTER_TEST_" + time.Now().Format("20060102150405")
	cleanupExportFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupExportFixture(t, pool, prefix) })

	data := createExportFixture(t, pool, prefix)
	handler := NewHandler(nil, nil, pool)

	fetch := func(extraQuery string) []string {
		request := httptest.NewRequest(http.MethodGet, "/api/admin/export/order-items.csv?cn="+data.cn+extraQuery, nil)
		response := httptest.NewRecorder()
		handler.OrderItems(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
		}
		records, err := csv.NewReader(strings.NewReader(response.Body.String()[3:])).ReadAll()
		if err != nil {
			t.Fatalf("read csv: %v", err)
		}
		names := []string{}
		for _, row := range records[1:] {
			names = append(names, row[5]) // 谷子名称 column
		}
		return names
	}

	// role=Rin should isolate "Partial Item" only (character_name = "Rin").
	if got := fetch("&role=Rin"); len(got) != 1 || got[0] != "Partial Item" {
		t.Fatalf("role=Rin -> %#v, want exactly [Partial Item]", got)
	}
	// category=badge should isolate "Unpaid Item" only (category = "badge").
	if got := fetch("&category=badge"); len(got) != 1 || got[0] != "Unpaid Item" {
		t.Fatalf("category=badge -> %#v, want exactly [Unpaid Item]", got)
	}
	// Combining a role and a category that don't both match the same item
	// must return nothing — filters AND together, they don't OR.
	if got := fetch("&role=Rin&category=badge"); len(got) != 0 {
		t.Fatalf("role=Rin&category=badge -> %#v, want empty (AND, not OR)", got)
	}
}

func TestOrderItemsCSVUnpaidOnlyFiltersByRemainingAmount(t *testing.T) {
	pool := newExportTestPool(t)
	prefix := "EXPORT_UNPAID_TEST_" + time.Now().Format("20060102150405")
	cleanupExportFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupExportFixture(t, pool, prefix) })

	data := createExportFixture(t, pool, prefix)
	handler := NewHandler(nil, nil, pool)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/export/order-items.csv?cn="+data.cn+"&unpaid_only=1", nil)
	response := httptest.NewRecorder()
	handler.OrderItems(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	body := response.Body.Bytes()
	if len(body) < 3 || body[0] != 0xef || body[1] != 0xbb || body[2] != 0xbf {
		t.Fatalf("CSV is missing UTF-8 BOM: % x", body[:min(3, len(body))])
	}
	records, err := csv.NewReader(strings.NewReader(string(body[3:]))).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("record count = %d, want header + 3 unpaid rows: %#v", len(records), records)
	}
	wantHeader := []string{"CN", "已付", "小计", "剩余", "付款状态", "谷子名称", "角色", "分类", "数量", "单价", "显示名称", "项目", "订单号", "来源文件", "来源 Sheet", "来源位置"}
	if strings.Join(records[0], "|") != strings.Join(wantHeader, "|") {
		t.Fatalf("header = %#v, want %#v", records[0], wantHeader)
	}

	byName := map[string][]string{}
	for _, row := range records[1:] {
		byName[row[5]] = row
	}
	if _, ok := byName["Paid Item"]; ok {
		t.Fatalf("paid item was exported: %#v", byName["Paid Item"])
	}
	assertCSVRow(t, byName["Unpaid Item"], data.cn, "Miku", "badge", "10.00", "0.00", "10.00", "未付款")
	assertCSVRow(t, byName["Partial Item"], data.cn, "Rin", "acrylic", "20.00", "5.00", "15.00", "部分付款")
	assertCSVRow(t, byName["Voided Item"], data.cn, "Len", "plush", "40.00", "0.00", "40.00", "未付款")
}

func TestOrderItemsExcelUnpaidOnlyHasFormattedWorkbook(t *testing.T) {
	pool := newExportTestPool(t)
	prefix := "EXPORT_UNPAID_TEST_" + time.Now().Format("20060102150405")
	cleanupExportFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupExportFixture(t, pool, prefix) })

	data := createExportFixture(t, pool, prefix)
	handler := NewHandler(nil, nil, pool)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/export/order-items.xlsx?cn="+data.cn+"&unpaid_only=1", nil)
	response := httptest.NewRecorder()
	handler.OrderItemsExcel(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("content type = %q", got)
	}
	files := readXLSXFiles(t, response.Body.Bytes())
	sheet := files["xl/worksheets/sheet1.xml"]
	styles := files["xl/styles.xml"]
	workbook := files["xl/workbook.xml"]
	if sheet == "" || styles == "" || workbook == "" {
		t.Fatalf("missing workbook parts: %#v", files)
	}
	for _, want := range []string{"谷子名称", "角色", "分类", "数量", "单价", "小计", "已付", "剩余", "来源位置", "Unpaid Item", "Partial Item", "Voided Item"} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("worksheet missing %q: %s", want, sheet)
		}
	}
	if strings.Contains(sheet, "Paid Item") {
		t.Fatalf("paid item should not be exported: %s", sheet)
	}
	for _, want := range []string{`<c r="I2" s="5"><v>1</v></c>`, `<c r="C2" s="3"><v>10.00</v></c>`, `<c r="B3" s="3"><v>5.00</v></c>`, `<c r="D3" s="3"><v>15.00</v></c>`} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("worksheet missing numeric cell %s: %s", want, sheet)
		}
	}
	for _, want := range []string{`<pane ySplit="1"`, `<autoFilter ref="A1:P4"`, `width="28.0"`, `customWidth="1"`} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("worksheet missing layout %s: %s", want, sheet)
		}
	}
	for _, want := range []string{`<left style="thin">`, `<right style="thin">`, `<top style="thin">`, `<bottom style="thin">`, `<alignment horizontal="center" vertical="center"`, `wrapText="1"`, `numFmtId="164"`, `numFmtId="1"`} {
		if !strings.Contains(styles, want) {
			t.Fatalf("styles missing %s: %s", want, styles)
		}
	}
	if !strings.Contains(workbook, `sheet name="导出"`) {
		t.Fatalf("workbook sheet name not localized: %s", workbook)
	}
}

type stubPaymentsStore struct {
	response payments.PaymentListResponse
}

func (s stubPaymentsStore) ListPaymentRecords(context.Context, payments.PaymentFilters) (payments.PaymentListResponse, error) {
	return s.response, nil
}

// TestPaymentsCSVFieldOrder locks in the exact first-8-column order required
// for human review: CN, 实付金额, 交肾状态, 本金, 手续费, 付款方式, 付款时间, 显示名称.
// Anything else (note, admin, void info) must come after these 8, never
// spliced in between.
func TestPaymentsCSVFieldOrder(t *testing.T) {
	store := stubPaymentsStore{response: payments.PaymentListResponse{
		Items: []payments.PaymentListItem{
			{
				CNCode:           "CN001",
				DisplayName:      "示例昵称",
				TotalAmount:      36.84,
				PrincipalAmount:  36.80,
				FeeAmount:        0.04,
				PaymentMethod:    "wechat",
				Status:           "approved",
				PaidAt:           "2026-07-12T15:39:00Z",
				CreatedBy:        "admin",
				Note:             "test note",
				PaymentItemCount: 2,
			},
		},
	}}
	handler := NewHandler(nil, store, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/export/payments.csv", nil)
	response := httptest.NewRecorder()
	handler.Payments(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	records, err := csv.NewReader(strings.NewReader(response.Body.String()[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("record count = %d, want header + 1 row", len(records))
	}
	wantFirst8 := []string{"CN", "实付金额", "交肾状态", "本金", "手续费", "付款方式", "付款时间", "显示名称"}
	if got := records[0][:8]; strings.Join(got, "|") != strings.Join(wantFirst8, "|") {
		t.Fatalf("first 8 header columns = %#v, want %#v", got, wantFirst8)
	}
	wantFirst8Values := []string{"CN001", "36.84", "已交肾", "36.80", "0.04", "微信", "2026-07-12 23:39:00", "示例昵称"}
	if got := records[1][:8]; strings.Join(got, "|") != strings.Join(wantFirst8Values, "|") {
		t.Fatalf("first 8 row values = %#v, want %#v", got, wantFirst8Values)
	}
}

// TestOrderItemsHeadersFirst10ColumnsMatchRequiredPriority locks in the
// exact first-10-column order for the unpaid-items export: CN, 已付, 小计,
// 剩余, 付款状态, 谷子名称, 角色, 分类, 数量, 单价.
func TestOrderItemsHeadersFirst10ColumnsMatchRequiredPriority(t *testing.T) {
	want := []string{"CN", "已付", "小计", "剩余", "付款状态", "谷子名称", "角色", "分类", "数量", "单价"}
	got := orderItemHeaders()[:10]
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("first 10 header columns = %#v, want %#v", got, want)
	}
}

type exportFixtureData struct{ cn string }

func createExportFixture(t *testing.T, pool *pgxpool.Pool, prefix string) exportFixtureData {
	t.Helper()
	ctx := context.Background()
	cn := prefix + "CN"
	projectCode := prefix + "PROJECT"
	orderNo := prefix + "ORDER"
	var userID, itemPartialID, itemPaidID, itemVoidedID string
	err := pool.QueryRow(ctx, `
		with u as (
			insert into users (cn_code, display_name, status)
			values ($1, 'Export unpaid test user', 'active')
			returning id
		), p as (
			insert into projects (code, name, status)
			values ($2, $2, 'active')
			returning id
		), product_unpaid as (
			insert into products (project_id, sku, name, character_name, category, unit_price, status, sort_order)
			select id, $2 || '_UNPAID', 'Unpaid Item', 'Miku', 'badge', 10, 'active', 1 from p
			returning id
		), product_partial as (
			insert into products (project_id, sku, name, character_name, category, unit_price, status, sort_order)
			select id, $2 || '_PARTIAL_LONG_SKU_123456789012345678901234567890', 'Partial Item', 'Rin', 'acrylic', 20, 'active', 2 from p
			returning id
		), product_paid as (
			insert into products (project_id, sku, name, character_name, category, unit_price, status, sort_order)
			select id, $2 || '_PAID', 'Paid Item', 'Luka', 'card', 30, 'active', 3 from p
			returning id
		), product_voided as (
			insert into products (project_id, sku, name, character_name, category, unit_price, status, sort_order)
			select id, $2 || '_VOIDED', 'Voided Item', 'Len', 'plush', 40, 'active', 4 from p
			returning id
		), o as (
			insert into orders (project_id, user_id, order_no, status, total_amount, note)
			select p.id, u.id, $3, 'partially_paid', 100, 'export unpaid test'
			from p cross join u
			returning id
		), item_unpaid as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, product_unpaid.id, 1, 10, 10, 'unpaid', 'Sheet1', 'R1' from o cross join product_unpaid
			returning id
		), item_partial as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, product_partial.id, 1, 20, 20, 'partial', 'Sheet1', 'R2-LONG-1234567890123456789012345678901234567890' from o cross join product_partial
			returning id
		), item_paid as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, product_paid.id, 1, 30, 30, 'paid', 'Sheet1', 'R3' from o cross join product_paid
			returning id
		), item_voided as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, product_voided.id, 1, 40, 40, 'unpaid', 'Sheet1', 'R4' from o cross join product_voided
			returning id
		)
		select (select id::text from u), (select id::text from item_partial), (select id::text from item_paid), (select id::text from item_voided)
	`, cn, projectCode, orderNo).Scan(&userID, &itemPartialID, &itemPaidID, &itemVoidedID)
	if err != nil {
		t.Fatalf("create export fixture: %v", err)
	}

	insertPayment := func(status string, amount string, key string, itemID string) {
		t.Helper()
		var paymentID string
		if err := pool.QueryRow(ctx, `
			insert into payments (user_id, submitted_amount, fee_amount, payable_amount, payment_method, status, submitted_at, approved_at, paid_at, idempotency_key)
			values ($1::uuid, $2::numeric(12,2), 0, $2::numeric(12,2), 'alipay', $3, now(), now(), now(), $4)
			returning id::text
		`, userID, amount, status, prefix+key).Scan(&paymentID); err != nil {
			t.Fatalf("insert payment %s: %v", key, err)
		}
		if _, err := pool.Exec(ctx, `
			insert into payment_items (payment_id, order_item_id, applied_amount)
			values ($1::uuid, $2::uuid, $3::numeric(12,2))
		`, paymentID, itemID, amount); err != nil {
			t.Fatalf("insert payment item %s: %v", key, err)
		}
	}
	insertPayment("approved", "5.00", "partial", itemPartialID)
	insertPayment("approved", "30.00", "paid", itemPaidID)
	insertPayment("voided", "40.00", "voided", itemVoidedID)
	return exportFixtureData{cn: cn}
}

func assertCSVRow(t *testing.T, row []string, cn string, character string, category string, amount string, paid string, remaining string, status string) {
	t.Helper()
	if len(row) == 0 {
		t.Fatalf("missing row for amount %s", amount)
	}
	if row[0] != cn || row[6] != character || row[7] != category || row[2] != amount || row[1] != paid || row[3] != remaining || row[4] != status || row[15] == "" {
		t.Fatalf("row = %#v, want cn %s character %s category %s amount/paid/remaining/status %s/%s/%s/%s with source", row, cn, character, category, amount, paid, remaining, status)
	}
}

func readXLSXFiles(t *testing.T, body []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("read xlsx zip: %v", err)
	}
	files := map[string]string{}
	for _, file := range reader.File {
		open, err := file.Open()
		if err != nil {
			t.Fatalf("open xlsx part %s: %v", file.Name, err)
		}
		data, err := io.ReadAll(open)
		_ = open.Close()
		if err != nil {
			t.Fatalf("read xlsx part %s: %v", file.Name, err)
		}
		files[file.Name] = string(data)
	}
	return files
}

// newExportTestPool returns a pool for this test's own throwaway database.
// It no longer loads backend/.env or reads DATABASE_URL, which pointed at the
// production database.
func newExportTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.New(t, "export")
}

func cleanupExportFixture(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`delete from payment_items where payment_id in (select p.id from payments p join users u on u.id = p.user_id where u.cn_code like $1)`,
		`delete from payments where user_id in (select id from users where cn_code like $1)`,
		`delete from order_items where order_id in (select o.id from orders o join users u on u.id = o.user_id where u.cn_code like $1)`,
		`delete from orders where user_id in (select id from users where cn_code like $1)`,
		`delete from products where project_id in (select id from projects where code like $1)`,
		`delete from projects where code like $1`,
		`delete from users where cn_code like $1`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement, prefix+"%"); err != nil {
			t.Fatalf("cleanup export fixture: %v", err)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
