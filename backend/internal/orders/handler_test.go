package orders

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

type stubStore struct {
	lastFilters  OrderFilters
	lastFacetReq FacetRequest
	response     OrderListResponse
}

func (s *stubStore) ListOrders(_ context.Context, filters OrderFilters) (OrderListResponse, error) {
	s.lastFilters = filters
	return s.response, nil
}

func (s *stubStore) OrderFacets(_ context.Context, request FacetRequest) (FacetResponse, error) {
	s.lastFacetReq = request
	return FacetResponse{Column: request.Column, Values: []FacetValue{}}, nil
}

func (s *stubStore) GetOrder(context.Context, string) (OrderDetailResponse, error) {
	return OrderDetailResponse{}, ErrOrderNotFound
}

func listRequest(t *testing.T, rawQuery string) (*stubStore, *httptest.ResponseRecorder) {
	t.Helper()
	store := &stubStore{response: OrderListResponse{Items: []OrderListItem{}}}
	handler := NewHandler(store)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/orders?"+rawQuery, nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)
	return store, recorder
}

func assertStrings(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

// TestListAcceptsMultipleCNValues covers the core WPS behaviour: ticking
// several CNs in one popover filters for all of them at once.
func TestListAcceptsMultipleCNValues(t *testing.T) {
	store, recorder := listRequest(t, "cn=CN001&cn=CN002&cn=CN003")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	assertStrings(t, store.lastFilters.CN, "CN001", "CN002", "CN003")
}

// TestListKeepsSeriesCategoryRoleIndependent guards the rule that 系列 / 团名 /
// 分类 / 谷子角色 are separate columns, each multi-valued, never folded into one
// combined search term. 系列 comes from the template's 分类(系列号) row, 团名 from
// the character header prefix ("25h miku" → "25h"); they must never share a
// filter.
func TestListKeepsSeriesCategoryRoleIndependent(t *testing.T) {
	store, recorder := listRequest(t, "series=26感谢祭&series=25生日&group=25h&group=ln&category=立牌&category=挂件&role=miku&role=rin")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	assertStrings(t, store.lastFilters.Series, "26感谢祭", "25生日")
	assertStrings(t, store.lastFilters.Group, "25h", "ln")
	assertStrings(t, store.lastFilters.Category, "立牌", "挂件")
	assertStrings(t, store.lastFilters.Role, "miku", "rin")
	if store.lastFilters.Item != nil {
		t.Fatalf("Item = %#v, want nil (series/category/role must not leak into the item column)", store.lastFilters.Item)
	}
}

func TestListCombinesMultipleColumns(t *testing.T) {
	store, _ := listRequest(t, "cn=CN001&status=paid&payment_status=partial&project=夏日祭&item=亚克力立牌")
	assertStrings(t, store.lastFilters.CN, "CN001")
	assertStrings(t, store.lastFilters.Status, "paid")
	assertStrings(t, store.lastFilters.PaymentStatus, "partial")
	assertStrings(t, store.lastFilters.Project, "夏日祭")
	assertStrings(t, store.lastFilters.Item, "亚克力立牌")
}

func TestListParsesAmountQuantityAndPaidRanges(t *testing.T) {
	store, recorder := listRequest(t, "amount_min=10.50&amount_max=999.99&quantity_min=1&quantity_max=20&paid_min=5&paid_max=100&unpaid_min=0&unpaid_max=50")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	filters := store.lastFilters
	// Ranges stay as decimal strings: they are cast to numeric in SQL, never
	// routed through float64.
	for _, pair := range [][2]string{
		{filters.AmountMin, "10.50"},
		{filters.AmountMax, "999.99"},
		{filters.QuantityMin, "1"},
		{filters.QuantityMax, "20"},
		{filters.PaidMin, "5"},
		{filters.PaidMax, "100"},
		{filters.UnpaidMin, "0"},
		{filters.UnpaidMax, "50"},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("range value = %q, want %q", pair[0], pair[1])
		}
	}
}

// TestListCreatedToCoversWholeDay pins the inclusive-day semantics: a user
// picking 2026-07-17 as the end date means "through the end of the 17th", so
// the exclusive bound must advance to the next midnight.
func TestListCreatedToCoversWholeDay(t *testing.T) {
	store, _ := listRequest(t, "created_from=2026-07-01&created_to=2026-07-17")
	if store.lastFilters.CreatedFrom != "2026-07-01T00:00:00Z" {
		t.Fatalf("CreatedFrom = %q", store.lastFilters.CreatedFrom)
	}
	if store.lastFilters.CreatedTo != "2026-07-18T00:00:00Z" {
		t.Fatalf("CreatedTo = %q, want the next midnight so the 17th is included", store.lastFilters.CreatedTo)
	}
}

func TestListRejectsInvalidParameters(t *testing.T) {
	cases := []struct{ name, query string }{
		{"non-numeric amount", "amount_min=abc"},
		{"negative amount", "amount_min=-5"},
		{"reversed amount range", "amount_min=20&amount_max=10"},
		{"reversed quantity range", "quantity_min=3&quantity_max=2"},
		{"bad date", "created_from=17/07/2026"},
		{"reversed date range", "created_from=2026-07-18&created_to=2026-07-17"},
		{"unknown status", "status=exploded"},
		{"unknown payment status", "payment_status=maybe"},
		{"page zero", "page=0"},
		{"page not a number", "page=abc"},
		{"page_size zero", "page_size=0"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, recorder := listRequest(t, testCase.query)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %q", recorder.Code, testCase.query)
			}
		})
	}
}

func TestListIgnoresEmptyAndWhitespaceValueParameters(t *testing.T) {
	store, recorder := listRequest(t, "cn=&cn=+++&series=+++")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if store.lastFilters.CN != nil || store.lastFilters.Series != nil {
		t.Fatalf("empty values should be ignored: %+v", store.lastFilters)
	}
}

// TestListRejectsOversizedPageSize is the guard against unbounded loading:
// an over-cap page_size is refused outright rather than silently clamped, so
// a caller cannot believe it received everything.
func TestListRejectsOversizedPageSize(t *testing.T) {
	_, recorder := listRequest(t, "page_size=9999")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	store, ok := listRequest(t, "page_size="+strconv.Itoa(MaxPageSize))
	if ok.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 at the cap", ok.Code)
	}
	if store.lastFilters.PageSize != MaxPageSize {
		t.Fatalf("PageSize = %d, want %d", store.lastFilters.PageSize, MaxPageSize)
	}
}

func TestListDefaultsPagination(t *testing.T) {
	store, _ := listRequest(t, "")
	if store.lastFilters.Page != 1 || store.lastFilters.PageSize != DefaultPageSize {
		t.Fatalf("page/page_size = %d/%d, want 1/%d", store.lastFilters.Page, store.lastFilters.PageSize, DefaultPageSize)
	}
}

// TestListResponseCarriesPaginationTotals checks the list reports the filtered
// total, not the page length, so "结果：共 N 项谷子明细" can be trusted. The unit
// is detail rows.
func TestListResponseCarriesPaginationTotals(t *testing.T) {
	store := &stubStore{response: OrderListResponse{
		Items:      []OrderListItem{{ItemID: "a"}, {ItemID: "b"}},
		Page:       2,
		PageSize:   50,
		Total:      137,
		TotalPages: 3,
	}}
	handler := NewHandler(store)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/orders?page=2", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	var body OrderListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 137 || body.TotalPages != 3 || body.Page != 2 || body.PageSize != 50 {
		t.Fatalf("pagination = %+v", body)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(body.Items))
	}
}

// TestListItemsCarryNoTechnicalIdentifiers locks the technical fields out of
// the list payload. The list is an operator-facing table; import batch ids and
// SKUs belong to the detail page's technical section only.
func TestListItemsCarryNoTechnicalIdentifiers(t *testing.T) {
	store := &stubStore{response: OrderListResponse{Items: []OrderListItem{{
		ItemID:  "11111111-1111-1111-1111-111111111111",
		OrderID: "22222222-2222-2222-2222-222222222222",
		CNCode:  "CN001", ProjectName: "夏日祭", ItemName: "初音未来吧唧",
	}}}}
	handler := NewHandler(store)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/orders", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, forbidden := range []string{"import_batch_id", "import_batch_ids", "import_filenames", "sku", "sha", "file_hash", "source_row_key"} {
		if _, present := body.Items[0][forbidden]; present {
			t.Fatalf("order list row exposes technical field %q", forbidden)
		}
	}
}

func TestListRejectsNonGET(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)
	recorder := httptest.NewRecorder()
	handler.List(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/orders", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}
