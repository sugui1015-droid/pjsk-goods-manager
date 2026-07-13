# Query browser test data helper

## 背景

- 为普通用户 `/query` 页面人工验收新增一次性测试数据工具。
- 测试数据统一限定在 `QUERY_BROWSER_TEST_20260713` 前缀下，避免修改真实用户、订单、商品、付款和 CN 数据。

## 变更

- 新增 `backend/cmd/query-browser-testdata`。
- `-mode=create` 会读取现有数据库配置，先清理同前缀旧测试数据，再创建最小完整验收数据：
  - 1 个测试用户；
  - 1 个项目；
  - 2 个商品；
  - 1 个订单；
  - 2 条订单明细；
  - 1 笔已生效付款，关联 2 条明细；
  - 1 笔已撤销付款，关联 1 条明细。
- `-mode=cleanup` 只删除 `users.cn_code` / `projects.code` 以指定前缀开头的数据及其关联测试行。
- 工具要求创建模式显式传入查询码，并只存储 bcrypt 哈希；本日志不记录数据库密码、查询码哈希、Cookie 或明文查询码。

## 校验

- 工具创建后在同一事务内校验：
  - `user_id`、`order_id`、`payment_id` 与 `payment_items` 关联存在；
  - 生效付款关联 2 条明细；
  - 撤销付款关联 1 条明细；
  - 部分付款明细有效已付金额为 4.00，状态为 `partial`；
  - 付清明细有效已付金额为 20.00，状态为 `paid`；
  - 订单状态为 `partially_paid`。

## 清理方案

验收完成后运行：

```powershell
cd D:\pjsk\backend
go run ./cmd/query-browser-testdata -mode=cleanup -prefix QUERY_BROWSER_TEST_20260713
```

该清理只使用上述前缀筛选测试数据，不会按真实 CN、订单号、商品名或付款 ID 做模糊删除。

## 2026-07-13 /query order summary acceptance and cleanup

- 原问题：普通用户 `/query` 订单卡片右上角摘要把总金额和件数连续显示，容易读成类似 `30.002 件` 的粘连数字。
- 页面修改：仅调整普通用户订单卡片顶部摘要区，改为四个独立标签块：`总金额`、`共 ... 件`、`已付`、`未付`；宽屏横向排列，窄屏自动换行；未改后端金额计算、数据库结构或管理员页面。
- 浏览器验收：Chrome 打开 `http://localhost:5173/query`，使用临时 CN `QUERY_BROWSER_TEST_20260713` 登录成功；页面显示订单数 1、总件数 2、总金额 30.00、已付金额 24.00、未付金额 6.00；订单卡片摘要不再出现总金额和件数粘连；付款历史 2 条关联明细可正常展开；320px 窄屏下摘要块自动换成 2 行且无重叠；Console error 列表为空；`/api/query/orders` 状态码 200。
- 清理前命中数量：users=1，projects=1，products=2，orders=1，order_items=2，payments=2，payment_items=3，query_sessions=3。
- 实际清理范围：在事务中删除测试 CN 及前缀 `QUERY_BROWSER_TEST_20260713` 关联的 query_sessions、payment_items、payments、order_items、orders、products、projects、users；未删除真实业务数据。
- 清理后确认：users=0，projects=0，products=0，orders=0，order_items=0，payments=0，payment_items=0，query_sessions=0；原临时 CN 登录返回 401。
- 真实业务总量核对：清理前 users=45、projects=2、products=71、orders=45、order_items=122、payments=9、payment_items=22、query_sessions=4；清理后 users=44、projects=1、products=69、orders=44、order_items=120、payments=7、payment_items=19、query_sessions=1，下降数量与测试数据完全一致。
- 额外数据操作：按用户要求创建真实 CN `succ` 并设置查询码哈希；不记录明文查询码、不记录哈希、不创建订单或付款；验证登录与 `/api/query/orders` 均返回 200，随后删除本次验证产生的 `succ` 查询会话，剩余会话 0。
- 测试和构建：`go fmt ./...`、`go build ./...`、`go vet ./...`、`go test ./...`、`pnpm run build` 均通过。
- Git 状态（提交前）：修改 `frontend/src/App.vue`、`frontend/src/style.css`，新增 `backend/cmd/query-browser-testdata/main.go` 与本开发日志；另有未跟踪的 `.claude/settings.local.json`，未纳入本轮变更。
