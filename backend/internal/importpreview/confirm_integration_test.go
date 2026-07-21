package importpreview

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pjsk/backend/internal/testdb"
)

// 这些测试跑真实的 confirm 持久化路径（SavePreview → ConfirmImport → 落库），
// 而不是只跑 parser。products 新增 group_name 之后，只有走到真实数据库才能
// 发现 INSERT 列表 / 占位符 / Scan 之间的不一致。
//
// 数据库来自 testdb：每个测试一个 pjsk_integration_test_* 库，schema 由真实
// 迁移生成，默认关闭（需要 PJSK_RUN_DB_INTEGRATION_TESTS=1）。

type confirmFixture struct {
	pool    *pgxpool.Pool
	store   *PostgresStore
	adminID string
	prefix  string
}

func newConfirmFixture(t *testing.T) confirmFixture {
	t.Helper()
	pool := testdb.New(t, "importconfirm")
	fixture := confirmFixture{
		pool:   pool,
		store:  NewPostgresStore(pool),
		prefix: fmt.Sprintf("IMPORT_CONFIRM_TEST_%d", time.Now().UnixNano()),
	}
	var adminID string
	if err := pool.QueryRow(context.Background(), `
		insert into admins (username, password_hash, status)
		values ($1, 'test-hash', 'active')
		returning id::text
	`, fixture.prefix+"_admin").Scan(&adminID); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	fixture.adminID = adminID
	return fixture
}

// standardPreview 造一份和真实标准模板同构的预览：B2「分类(系列号)」= 63色纸，
// 角色列里既有复合列头 "25h miku"（团名 25h），也有纯角色 "szk"（团名为空）。
func standardPreview(t *testing.T, filename string, fileHash string) Preview {
	t.Helper()
	data := testWorkbook(t, testSheet{
		Name: "vol.63色纸",
		Rows: [][]any{
			{"【谷子名字】汇总详情"},
			{nil, "分类(系列号)", "63色纸"},
			{nil, "种类（角色名）", "25h miku", "szk"},
			{nil, "单价", 10, 20},
			{"总金额", "昵称/总数", 1, 1},
			{30, "柴", 1, 1},
		},
	})
	preview, err := Parse(data, ParseOptions{Filename: filename, FileHash: fileHash, Size: int64(len(data))})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return preview
}

type storedProduct struct {
	Name          string
	Category      string
	SeriesCode    string
	GroupName     string
	CharacterName string
}

func (f confirmFixture) productsForProject(t *testing.T, projectID string) map[string]storedProduct {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		select coalesce(name, ''), coalesce(category, ''), coalesce(series_code, ''),
		       coalesce(group_name, ''), coalesce(character_name, '')
		from products
		where project_id = $1::uuid
	`, projectID)
	if err != nil {
		t.Fatalf("select products: %v", err)
	}
	defer rows.Close()
	found := map[string]storedProduct{}
	for rows.Next() {
		var product storedProduct
		if err := rows.Scan(&product.Name, &product.Category, &product.SeriesCode, &product.GroupName, &product.CharacterName); err != nil {
			t.Fatalf("scan product: %v", err)
		}
		found[product.CharacterName] = product
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return found
}

// 主用例：标准模板预览 → 确认导入 → products 五个字段全部落库正确。
// 同时覆盖 group_name 为空（szk）和非空（25h miku）两种插入。
func TestConfirmImportPersistsGroupNameAndSeries(t *testing.T) {
	fixture := newConfirmFixture(t)
	hash := fixture.prefix + "-hash"
	preview := standardPreview(t, "标准模板.xlsx", hash)

	state, err := fixture.store.SavePreview(context.Background(), preview, fixture.adminID)
	if err != nil {
		t.Fatalf("SavePreview: %v", err)
	}

	result, err := fixture.store.ConfirmImport(context.Background(), state.ImportBatchID, fixture.adminID, true, ConfirmRules{})
	if err != nil {
		t.Fatalf("ConfirmImport: %v", err)
	}
	if result.OrderItemCount != 2 {
		t.Fatalf("order item count = %d, want 2", result.OrderItemCount)
	}
	if result.CNCount != 1 || result.OrderCount != 1 {
		t.Fatalf("cn=%d orders=%d, want 1/1", result.CNCount, result.OrderCount)
	}
	if result.ProductCount != 2 {
		t.Fatalf("product count = %d, want 2", result.ProductCount)
	}

	products := fixture.productsForProject(t, result.ProjectID)
	if len(products) != 2 {
		t.Fatalf("products = %#v, want 2", products)
	}

	// 复合列头：团名 25h，角色 miku。
	miku, ok := products["miku"]
	if !ok {
		t.Fatalf("miku product missing: %#v", products)
	}
	if miku.GroupName != "25h" {
		t.Fatalf("miku group_name = %q, want 25h", miku.GroupName)
	}
	// 纯角色列头：团名为空，角色仍要落库。
	szk, ok := products["szk"]
	if !ok {
		t.Fatalf("szk product missing: %#v", products)
	}
	if szk.GroupName != "" {
		t.Fatalf("szk group_name = %q, want empty", szk.GroupName)
	}

	// 系列与分类都来自 B2「分类(系列号)」；谷子名称在 A1 是占位词时兜底到工作表名。
	for character, product := range products {
		if product.SeriesCode != "63色纸" {
			t.Fatalf("%s series_code = %q, want 63色纸", character, product.SeriesCode)
		}
		if product.Category != "63色纸" {
			t.Fatalf("%s category = %q, want 63色纸", character, product.Category)
		}
		if product.Name != "vol.63色纸" {
			t.Fatalf("%s name = %q, want 工作表名 vol.63色纸", character, product.Name)
		}
	}
}

// 事务完整性：确认过程中途失败时，projects / products / orders / order_items
// 都不能残留半批数据，import_batches 也不能停在 processing。
func TestConfirmImportRollsBackEverythingOnFailure(t *testing.T) {
	fixture := newConfirmFixture(t)
	hash := fixture.prefix + "-rollback-hash"
	preview := standardPreview(t, "回滚用例.xlsx", hash)

	state, err := fixture.store.SavePreview(context.Background(), preview, fixture.adminID)
	if err != nil {
		t.Fatalf("SavePreview: %v", err)
	}

	// 制造后续写入失败：order_items 上加一个必然违反的约束，
	// 它只会在明细写入阶段触发，此时 project / product / order 已经写过了。
	if _, err := fixture.pool.Exec(context.Background(), `
		alter table order_items
		add constraint order_items_rollback_probe check (quantity < 0) not valid
	`); err != nil {
		t.Fatalf("add probe constraint: %v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `
		alter table order_items validate constraint order_items_rollback_probe
	`); err != nil {
		// 表里本来就没有数据，validate 应当成功；失败说明用例前提不成立。
		t.Fatalf("validate probe constraint: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(context.Background(), `alter table order_items drop constraint if exists order_items_rollback_probe`)
	})

	if _, err := fixture.store.ConfirmImport(context.Background(), state.ImportBatchID, fixture.adminID, true, ConfirmRules{}); err == nil {
		t.Fatal("ConfirmImport succeeded, want failure from the probe constraint")
	}

	for _, table := range []string{"order_items", "orders", "products", "projects"} {
		var count int
		if err := fixture.pool.QueryRow(context.Background(), fmt.Sprintf(`select count(*) from %s`, table)).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s has %d rows after rollback, want 0", table, count)
		}
	}

	var status string
	if err := fixture.pool.QueryRow(context.Background(), `
		select status from import_batches where id = $1::uuid
	`, state.ImportBatchID).Scan(&status); err != nil {
		t.Fatalf("select batch status: %v", err)
	}
	if status != "previewed" {
		t.Fatalf("batch status = %q, want previewed (processing 说明事务没回滚干净)", status)
	}
}

// 旧格式（矩阵型）文件的确认导入必须仍然成功：它没有 B2 那一行，
// series_code 走的是表头里的系列码，group_name 为空。
func TestConfirmImportLegacyMatrixStillWorks(t *testing.T) {
	fixture := newConfirmFixture(t)
	data := testWorkbook(t, testSheet{
		Name: "旧格式",
		Rows: [][]any{
			{"旧版汇总"},
			{nil, "分类", "63a"},
			{nil, "种类", "miku", "rin"},
			{nil, "单价", 10, 20},
			{"总金额", "昵称/总数", 1, 1},
			{30, "Alice", 1, 1},
		},
	})
	preview, err := Parse(data, ParseOptions{Filename: "旧格式.xlsx", FileHash: fixture.prefix + "-legacy", Size: int64(len(data))})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	state, err := fixture.store.SavePreview(context.Background(), preview, fixture.adminID)
	if err != nil {
		t.Fatalf("SavePreview: %v", err)
	}
	result, err := fixture.store.ConfirmImport(context.Background(), state.ImportBatchID, fixture.adminID, true, ConfirmRules{})
	if err != nil {
		t.Fatalf("ConfirmImport legacy: %v", err)
	}
	if result.OrderItemCount != 2 {
		t.Fatalf("legacy order item count = %d, want 2", result.OrderItemCount)
	}
	products := fixture.productsForProject(t, result.ProjectID)
	if len(products) != 2 {
		t.Fatalf("legacy products = %#v, want 2", products)
	}
	for character, product := range products {
		if product.GroupName != "" {
			t.Fatalf("legacy %s group_name = %q, want empty", character, product.GroupName)
		}
	}
}

// 复现并锁定线上那次 500：批次被撤销后再点「确认导入」。
// 这条路径以前是一个裸 fmt.Errorf，匹配不到任何 sentinel，落进 default 分支，
// 于是变成 500 + 日志只有 "confirm import: internal error"。
// 它其实是业务冲突，应当是 409，而且日志要能定位。
func TestConfirmImportAfterRevokeIsConflictNotInternalError(t *testing.T) {
	fixture := newConfirmFixture(t)
	preview := standardPreview(t, "撤销后再确认.xlsx", fixture.prefix+"-revoked")

	state, err := fixture.store.SavePreview(context.Background(), preview, fixture.adminID)
	if err != nil {
		t.Fatalf("SavePreview: %v", err)
	}
	if _, err := fixture.store.ConfirmImport(context.Background(), state.ImportBatchID, fixture.adminID, true, ConfirmRules{}); err != nil {
		t.Fatalf("first ConfirmImport: %v", err)
	}
	if _, err := fixture.store.RevokeImport(context.Background(), state.ImportBatchID, fixture.adminID); err != nil {
		t.Fatalf("RevokeImport: %v", err)
	}

	_, err = fixture.store.ConfirmImport(context.Background(), state.ImportBatchID, fixture.adminID, true, ConfirmRules{})
	if err == nil {
		t.Fatal("confirm after revoke succeeded, want a conflict error")
	}
	var notConfirmable *ImportNotConfirmableError
	if !errors.As(err, &notConfirmable) {
		t.Fatalf("error = %#v, want *ImportNotConfirmableError so the handler can answer 409", err)
	}
	if notConfirmable.Status != "reverted" {
		t.Fatalf("status = %q, want reverted", notConfirmable.Status)
	}
}
