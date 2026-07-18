# 2026-07-16 静态收款二维码与页面结构优化

本日志记录「静态收款二维码 MVP + 页面精简分块 + 对应数据独立筛选 + 登录入口分离」
这一轮的调查、设计与影响范围分析。

- 分支：main
- 基线 HEAD = origin/main = `c8e7df0d7143137b8f6374d2b376a42bb8530582`
- 正式入口：http://127.0.0.1:8081（本机/内网，已验收）
- 最高迁移：0019；新迁移必须从 0020 开始
- 历史两个 0005（`0005_import_history.sql` / `0005_product_series.sql`）不动
- 是否使用子代理：否（全程单会话只读调查）
- 本轮是否修改真实业务数据：否（第一停止点前仅调查，无代码/迁移/提交）

---

## 阶段一：只读调查结果

### 1. 前端整体结构

前端是**单文件 SPA**：`frontend/src/App.vue`（约 3311 行），一个 `<script setup>`
包含全部管理员端与普通用户端逻辑、模板、样式。没有 vue-router，没有拆分组件
（`components/HelloWorld.vue` 是脚手架遗留，未使用）。

路由靠手写 `history.pushState` + `routeFromPath(window.location.pathname)` 实现
（`App.vue:341-353`）：

| 路径 | routeName | 归属 |
| --- | --- | --- |
| `/` | `home` | 公开：开发者总览（模块状态/接口/旧版命令） |
| `/query` | `query` | 普通用户：CN + 查询码登录与订单/付款查询 |
| `/admin/imports` | `admin-imports` | 管理员：Excel 导入预览 |
| `/admin/imports/history`,`/admin/imports/{id}` | 导入历史/详情 | 管理员 |
| `/admin/orders`,`/admin/orders/{id}` | 订单查询/详情 | 管理员 |
| `/admin/users`,`/admin/users/{id}` | 用户管理/详情 | 管理员 |
| `/admin/payments`,`/admin/payments/{id}` | 付款记录/详情 | 管理员 |

`isAdminRoute = routeName !== 'home' && routeName !== 'query'`（`App.vue:269`）。
模板顶层：`<main v-if="isAdminRoute">`（管理员）/ `<main v-else>`（home 或 query）。

后端是**纯 API 服务**（`backend/main.go`）：只 `//go:embed migrations/*.sql`，
**不托管前端、不提供任何静态文件服务**。前端由 Caddy/Vite 独立提供
（`deploy/caddy`、`deploy/nginx` 存在）。→ 二维码图片只能经后端 API 输出，
不能走静态目录。

### 2. 普通用户当前能看到什么（`/query`）

来源：`GET /api/query/orders`（`backend/internal/query/handler.go`）。登录后展示：

- **汇总区**（`App.vue:3181-3188`，一个 `summary-grid`）：CN、订单数、总件数、
  **总金额**、**已付金额**、**未付金额**（`remaining_amount`，标红）。
- **每个订单卡片**：桌面表格 + 移动卡片，逐条明细含 谷子名称/系列/分类/角色/数量/
  单价/小计/已付/剩余未付/付款状态。
- **付款历史**（`App.vue:3243-3294`）：付款时间、实付金额、交肾状态、本金、手续费、
  付款方式、可展开的关联明细（谷子名称/角色/分类/数量/单价/小计/本次付款金额/状态）。
  无记录时显示「暂无付款记录」。

用户看不到的字段（后端已在 DTO 层裁剪，`query.OrderItem`/`query.PaymentItem`
注释明确说明）：订单号、项目名、order_item id、import_batch、source_sheet、
SKU、数据库 ID、管理员用户名、备注、撤销原因。**这一裁剪要在新增二维码/付款区时严格保持。**

**当前普通用户没有「付款方式选择 + 收款二维码」区域**，也没有任何列表筛选。

### 3. 管理员付款录入与管理流程

- **录入付款**（`/admin/payments` 页内「录入付款」卡片，`App.vue:2616-2673`）：
  按 CN 加载未付明细 → 勾选明细并逐条填分摊金额 → 选付款方式（当前 UI 只给
  **支付宝 / 微信** 两个 radio，`App.vue:2640-2647`）→ 核对本金/手续费/实付 → 确认。
  提交 `POST /api/admin/payments`，后端直接以 `status='approved'` 落库
  （`payments/handler.go:CreatePayment`，行 633-651）。带幂等键 + `pg_advisory_xact_lock`。
- **手续费**：整数分计算。支付宝 0；微信 0.1% 向上取整（`calculateFee`，行 1379-1390）。
- **付款方式规范化**：`wechat/alipay/bank/cash/other`（`normalizePaymentMethod`，行 1338）。
  DB 层无枚举约束，靠应用层校验。
- **付款记录**（`ListPaymentRecords`）、**详情**（`GetPaymentDetail`）、
  **撤销/作废**（`VoidPayment`：仅 `approved`→`voided`，需理由，记录 `voided_by_admin_id`/
  `void_reason`/`voided_at`，重算用户明细状态）。无删除。
- 管理员付款相关接口全部挂在 `adminHandler.RequireAuthentication` 后。

### 4. 数据库付款相关表结构（`migrations/0001` + 0009~0012）

`payments`（关键列）：
`id, user_id, submitted_amount(本金), fee_amount, payable_amount(=本金+手续费),
payment_method(text, 无枚举约束), screenshot_storage_path(text, 见下), note,
status(submitted/approved/rejected/cancelled/voided), submitted_at, approved_at,
approved_by, rejected_*, paid_at, created_by(admins), idempotency_key(唯一),
voided_at, voided_by_admin_id, void_reason, created_at, updated_at`。

`payment_items`：`id, payment_id, order_item_id, applied_amount,
unique(payment_id, order_item_id)`。

**重要**：`payments.screenshot_storage_path` 列已存在（0001 起）但**当前完全未使用**
（代码无任何读写）。这是历史为「用户上传付款截图」预留的字段 —— 本轮明确不做截图上传，
且**收款二维码是管理员配置的固定收款码，与该字段语义完全不同**，不要复用它。

### 5. 现有图片上传 / 静态存储 / 附件处理基础

- **唯一的文件上传路径**：Excel 导入预览（`importpreview/handler.go`）。
  用 `r.ParseMultipartForm` + `r.FormFile("file")`，`maxPreviewFileSize = 20<<20`（20MB），
  `http.MaxBytesReader`。**格式校验仅靠扩展名 `.xlsx`**（行 87-88），随后交给 xlsx 解析器
  ——没有基于文件内容的魔数校验。Excel 文件不落盘，解析后只存结构化数据。
- **没有任何图片存储、静态文件目录服务、附件表**。
- `.gitignore` 已含 `# Local app data and uploaded payment images` 段，忽略 `data/`、
  `uploads/`、`*.csv` 等 —— 说明历史上为「本地上传图片」预留过目录约定，但从未实现。

### 6. API 请求体限制 / MIME 校验现状

- JSON 接口：`http.MaxBytesReader(w, r.Body, 1<<20)`（1MB）+ `DisallowUnknownFields`
  +「必须恰好一个 JSON 对象」（payments/query 的 `decodeJSON`）。
- multipart（仅导入）：20MB。
- **全项目无 `http.DetectContentType`、无图片魔数校验、无 MIME 白名单**。
  二维码上传需要新引入内容级校验（PNG/JPEG/WebP 魔数）。

### 7. 管理员审计基础能否复用

现有审计是**专用**表 `admin_auth_audit_events`（0019），event_type 有严格 CHECK
约束，只覆盖登录/登出/限流四类，且明确「不存 body/密码/token」。
它**不是通用审计表**，不能直接塞二维码事件（会违反 CHECK 约束）。

二维码的「谁在何时更新」审计，推荐**直接写进二维码表自身的
`updated_by` / `updated_at` 元数据 + 保留历史行**（软替换而非物理删除），
符合任务「不直接删除历史审计证据」「查看更新时间/更新管理员」的要求，无需新建审计表。
`admin.CurrentAdmin(ctx)` 已能在鉴权后取到当前管理员 ID（付款/撤销就是这样用的），
可直接复用作 `updated_by`。

### 8. 页面分块问题清单（现状痛点）

1. **顶部管理员导航 `nav.tabs`（`App.vue:2378-2385`）在所有页面都渲染**，包括 `/query`
   和 `/`。普通用户访问 `/query` 会看到「Excel 导入预览/导入历史/用户管理…」等管理员标签
   —— 入口混淆、观感差（点了会撞管理员鉴权）。
2. **没有统一入口页**：管理员登录是嵌在 `/admin/*` 里的内联表单（`!admin` 时显示，
   `App.vue:2393-2401`），普通用户登录在 `/query`。二者视觉/信息架构割裂，无分流页。
3. **home（`/`）是开发者总览**（模块状态/接口清单/Streamlit 旧版命令），
   对真实终端用户无意义，却是根路径首屏。
4. 普通用户 `/query` 页把「登录卡 / 账号安全 / 汇总 / 每个订单 / 付款历史」纵向堆叠，
   块与块之间层级尚可，但**汇总区是单一 `summary-grid` 六格挤在一起**，未按
   「付款汇总」独立成组；**无付款方式选择与二维码区**。
5. **方框尺寸不统一**：`metric-tile`（汇总格）、`amount-card`、`panel`、
   `payment-method-option` 各有各的尺寸/内边距，文字未强制居中，长短不一
   —— 对应用户「方框大小一致、文字居中完美嵌合」的诉求。
6. **筛选缺失/错配**：
   - 普通用户订单区、付款历史区**完全没有筛选**（数据多时无法定位）。
   - 管理员「用户管理」筛选只有 `{cn, status}`（`App.vue:110`），缺「是否设查询码/
     是否绑邮箱/是否有订单/是否有未付」等业务维度。
   - 管理员「付款记录」筛选（CN/方式/状态/时间）与上方「录入付款」共处一个 `panel`，
     但两者数据不同源，筛选归属需更清晰。
7. **移动端 320px**：多个宽表（订单/付款/导入预览）靠 `table-scroll` 横向滚动；
   汇总 `summary-grid` 在窄屏是否换行需逐个核对。二维码卡片新增时要保证 320px 纵向不横滚。

### 9. 现有筛选字段盘点

| 区域 | 现有筛选 | 状态定义位置 |
| --- | --- | --- |
| 导入预览 | Sheet/大标题/批次/CN/分类/角色/名称/来源 + 数量/价格/小计范围 + 排除态/分类态 | `App.vue:306-332` |
| 管理员订单 | CN/项目/名称/系列/分类/角色/导入批次/状态/创建时间起止 | `orderFilters` `App.vue:181-192` |
| 管理员付款记录 | CN/付款方式/状态/付款时间起止 | `paymentFilters` `App.vue:214-220` |
| 管理员用户 | CN/状态 | `adminUserFilters` `App.vue:110` |
| 普通用户订单 | 无 | — |
| 普通用户付款历史 | 无 | — |

任务列出的目标字段（CN/分类/角色/系列/付款状态/付款方式/时间）后端多数已支持：
- 订单列表后端已接受 category/role/series/status/时间等参数（`orders` handler，待细查参数名）。
- 付款记录后端 `PaymentFilters` 已支持 cn/payment_method/status/paid_from/paid_to。
- 普通用户 `/api/query/orders` **一次性返回该 CN 全部订单**，数据量天然有界（单用户），
  故普通用户端筛选可安全地在**前端**做（符合任务第 11 条：数据量大才必须后端筛选；
  单用户订单量小）。管理员端列表跨用户、量大，应继续走**后端**筛选。

### 10. 静态二维码存储方案对迁移 Linux 的影响

- 备份机制：`scripts/database/Backup-Postgres.ps1` 用
  `pg_dump --format=custom --no-owner --no-privileges`（**整库逻辑备份，无表排除**）。
- 因此：**若二维码存 `bytea` 到数据库，图片自动进入现有 pg_dump 备份**，迁移 Linux
  只需 `pg_restore`，零额外文件搬运、与元数据强一致、可回滚。
- 若存受控数据目录（`data/`），则需**另建**文件备份/同步与恢复流程，pg_dump 不覆盖它，
  迁移时容易漏；且后端当前不服务静态文件，仍要写读取 API。
- 结论详见「阶段三」。

---

## 阶段三：存储方案决策（bytea vs 数据目录）

**推荐：`bytea` 存数据库。** 理由：

1. 体量极小：仅两张收款码（支付宝/微信），单张 ≤5MB，DB 膨胀可忽略。
2. 备份/迁移一体化：现有 pg_dump 整库备份**自动包含**二维码，Linux 迁移即 `pg_restore`，
   无需为图片单独设计备份，契合任务「需要完整备份迁移」。
3. 强一致 + 可审计：图片与 `updated_by/sha256/enabled` 元数据在同一行、同一事务，
   替换/停用可原子完成，历史行可保留。
4. 安全：不落文件系统，杜绝「用户可控文件名拼服务器路径」「静态目录被直链」类风险；
   读取只经受控 API，可精确控制鉴权、Content-Type、nosniff、Cache-Control。
5. 后端本就不服务静态文件，走 API 输出图片与现有架构一致。

代价（可接受）：图片经 DB 读出而非 nginx 直发 —— 但只有两张小图、访问量极低，
可用 `ETag`/`Cache-Control` 缓解。

---

## 阶段三：二维码数据模型设计（迁移 0020）

新迁移 `backend/migrations/0020_payment_qr_codes.sql`（从 0020 起，不碰历史）：

```
create table if not exists payment_qr_codes (
    id            uuid primary key default gen_random_uuid(),
    payment_method text not null check (payment_method in ('alipay','wechat')),
    image_data    bytea not null,
    mime_type     text not null check (mime_type in ('image/png','image/jpeg','image/webp')),
    byte_size     integer not null check (byte_size > 0 and byte_size <= 5242880),
    sha256        text not null check (char_length(sha256) = 64),
    enabled       boolean not null default true,
    updated_by    uuid references admins(id) on delete set null,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now()
);
-- 每种方式最多一条「当前生效」记录：
create unique index if not exists payment_qr_codes_active_method_unique
    on payment_qr_codes(payment_method) where enabled = true;
create index if not exists payment_qr_codes_method_updated_idx
    on payment_qr_codes(payment_method, updated_at desc);
```

设计要点：
- 「每方式最多一个生效码」用 `partial unique index (where enabled)` 保证，而非删除旧行 ——
  停用=把旧行 `enabled=false` 保留为历史证据，替换=停用旧行+插入新行（一个事务）。
- `payment_method` CHECK 仅 `alipay/wechat`（本轮不含 bank/cash/other 的收款码）。
- `mime_type` CHECK 仅三种；`byte_size` CHECK ≤5MB；`sha256` 存内容哈希用于去重/审计，
  **日志只记 sha256/大小/类型/时间，绝不记图片二进制**。
- 为未来「动态二维码」预留：可后续加 `code_type text default 'static'` 等列，本轮不加。
- 不在此表存文件系统路径，无「用户可控文件名」概念。

---

## 需要的新表与 API

**新表**：`payment_qr_codes`（见上，唯一迁移 0020）。

**管理员 API**（均挂 `RequireAuthentication`）：
- `GET  /api/admin/payment-qr` —— 返回两种方式的当前状态（enabled/更新时间/更新管理员/
  mime/大小/sha256），**不返回二进制、不返回路径**。
- `POST /api/admin/payment-qr/{method}` —— multipart 上传，`method∈{alipay,wechat}`；
  内容级魔数校验 PNG/JPEG/WebP + ≤5MB；成功=停用旧生效码并插入新行，写 `updated_by`。
- `POST /api/admin/payment-qr/{method}/disable` —— 停用当前生效码（保留历史行）。
- `GET  /api/admin/payment-qr/{method}/image` —— 管理员预览图片（受鉴权），
  正确 `Content-Type` + `X-Content-Type-Options: nosniff` + 合理 `Cache-Control`。

**普通用户 API**（需查询会话，登录后可读）：
- `GET  /api/query/payment-qr/{method}/image` —— 输出当前生效二维码二进制，
  同样带正确 Content-Type + nosniff + Cache-Control；未配置返回 404（前端转空状态）。
- 或合并：`GET /api/query/payment-qr` 返回「哪些方式已配置」的布尔状态，前端据此决定
  是否请求图片。二维码**不对未登录用户开放**（复用查询会话中间件）。

上传大小：为二维码单独设 `maxQRUploadSize = 5<<20`，不沿用导入的 20MB。

---

## 管理员二维码页面设计（阶段四）

新增管理员路由 `/admin/payment-qr`（routeName `admin-payment-qr`），在导航加「收款二维码」。
页面两张等宽卡片，320px 纵向排列、无横滚：

- 【支付宝收款码】克制蓝色标识：当前状态（已配置/未配置/已停用）、图片预览、
  上传/替换（前置预览 + 显示文件名/格式/大小）、停用、更新时间。
- 【微信收款码】克制绿色标识：同上。
- 替换/停用二次确认；上传成功即刷新状态；失败中文提示。
- 数据库 ID/SHA/字节数等技术信息放入**默认收起**的「技术详情」，不显示文件路径。

## 普通用户付款区域设计（阶段五）

在 `/query` 登录后、订单卡片区附近新增分块（每块独立 `panel`/卡片）：

- 【付款汇总】总金额/已付/未付/件数，**四个独立等尺寸方框**（统一 `metric-tile` 规格，
  文字居中）。
- 【选择付款方式】支付宝（蓝）/微信（绿）两个**等宽**按钮，仅显示已配置且启用的方式。
- 【本次应付】放大加粗显示未付金额，只读，附「仍由管理员核对后录入」说明。
- 【收款二维码】按选中方式显示对应图片，完整不拉伸，点击放大；未配置显示明确空状态
  「管理员暂未配置该付款方式的收款二维码。」；附提示「请按页面显示金额付款，
  付款完成不代表系统已自动确认。」
- 【付款历史】沿用现有展开逻辑，无记录显示「暂无付款记录」，只展示业务字段。

本轮**不加**「我已付款」按钮、不上传截图、不创建待审核付款、扫码不改状态。

---

## 页面分块与「方框统一」实现思路（阶段六 + 小功能）

- 统一一套卡片/方框规范：定义 `.metric-tile` 固定 `min-height` + `display:flex;
  flex-direction:column; align-items:center; justify-content:center; text-align:center`，
  让同类数据框等高、文字水平垂直居中；`panel` 统一圆角/边框/内边距/间距层级。
- 按钮体系：主按钮/次按钮/危险按钮分离，同类等宽等高。
- 技术信息统一收进 `<details>` 默认收起；普通用户端完全不渲染技术字段。
- 空/加载/失败三态分别渲染。
- 用间距/边框/标题/背景层级区分区域，不靠堆颜色。

---

## 筛选矩阵（阶段七）

每个列表在自己卡片顶部拥有**只控制本列表**的筛选区，含「当前条件可见 + 清空筛选」，
选项中文，桌面尽量一行、320px 纵向：

| 列表 | 筛选字段 | 前/后端 |
| --- | --- | --- |
| 管理员用户 | CN、查询权限、是否设查询码、是否绑恢复邮箱、是否有订单、是否有未付 | 后端（需补参数） |
| 管理员订单 | CN、分类、角色、系列、付款状态、金额范围、导入批次/时间 | 后端（多数已有） |
| 管理员未付明细 | CN、分类、角色、系列、未付金额范围 | 后端（需补） |
| 管理员付款记录 | CN、付款方式、状态、付款时间、金额范围 | 后端（金额范围需补） |
| 导入历史 | 文件名、Sheet、导入时间、状态、有无错误/警告 | 后端（需细查） |
| 普通用户订单 | 分类、角色、系列、付款状态 | 前端（单用户数据小） |
| 普通用户付款历史 | 付款方式、状态、时间 | 前端 |

原则：汇总口径明确区分「全部数据」与「当前筛选结果」（导入预览已有此范式，可借鉴）；
不把内部技术字段作为普通用户筛选项；优先复用现有后端参数，缺失才补 API。

---

## 登录入口重做（用户/管理员分离）

### 现有登录结构
- 管理员登录：内联在 `/admin/*` 路由，`!admin` 时渲染 `auth-panel`
  （`App.vue:2393-2401`），字段 用户名/密码 → `POST /api/admin/login`（HttpOnly Cookie、
  限流、审计 0019 已具备）。
- 普通用户登录：`/query` 的 `query-panel`，字段 CN/查询码 → `POST /api/query/login`，
  附「忘记查询码 / 首次设置查询码（绑定码）/ 邮箱找回」入口。
- 二者视觉割裂，且管理员导航标签在所有页面渲染，无分流首页。

### 推荐信息架构
1. **统一入口页**（新 `/`，routeName `home` 改造）：品牌「PJSK 谷子系统」+ 副标题
   「请选择登录入口」+ 两张等尺寸入口卡片：
   - 普通用户入口 → `/query`
   - 管理员入口（视觉更克制）→ `/admin`（新登录页）
   现开发者总览下沉到 `/admin` 登录后或移除，不再占首屏。
2. **普通用户登录页** `/query`：居中大卡片，品牌标题「PJSK 谷子系统」+ 副标题「用户服务入口」，
   CN + 查询码输入、宽登录按钮、找回/首次设置/邮箱找回入口收拢在说明区。
   登录后进入现有业务页，业务逻辑不变。
3. **管理员登录页** `/admin`（把当前内联表单独立成页）：同一视觉语言，品牌标题
   「PJSK 谷子系统」+ 副标题「管理员入口」，账号 + 密码，登录后进现有后台，保留限流/401/会话/审计。
4. 两页复用同一套卡片/输入框/按钮尺寸规范（**仅借鉴**参考图片/参考站点的布局要点：
   居中大卡、大标题+副标题、圆角统一、留白充足、移动端友好），但**不逐像素照搬**，
   也**不复制参考来源的任何品牌名/业务名**（如「音游窝」）、不引入「选择店铺/
   手机号登录/记住密码/免登录天数」等本系统没有的元素。

> **品牌与参考来源边界（2026-07-16 更正）**：全系统主页与两类登录页统一使用本系统名称
> **「PJSK 谷子系统」**。用户提供的登录截图与参考站点 https://rensheet.top/ **只作为布局/
> 视觉风格参考**，不得作为品牌或文字内容来源；参考来源中出现的「音游窝」等品牌文字一律
> 不得出现在本系统任何界面。阶段五按用户 2026-07-16 更新后的指令执行（详见「阶段五」实现记录）。

### 路由与兼容
- 新增 `home`（分流页）、`/admin`（管理员登录页，未登录时）；`/admin/*` 深链在未登录时
  统一跳到 `/admin` 登录。
- 旧地址：`/query` 保留不变；`/` 从「开发者总览」变为「分流页」——建议保留 `/`，
  旧收藏仍可用，只是内容改版。管理员深链维持原路径，登录态判断照旧。
- 响应式：桌面两卡并排；768px 两卡自适应；320px 纵向堆叠、按钮全宽、无横滚。

### 是否需要新增登录页相关测试
需要：分流页两入口可达、普通/管理员登录页 320px 契约、管理员未登录深链跳转、
入口分离后既有登录逻辑（限流/401/找回入口）不回归。

---

## 安全风险清单

1. **图片伪装**：必须按文件内容魔数校验 PNG/JPEG/WebP，拒绝 SVG/HTML/脚本/可执行/
   伪装图片；只信内容不信扩展名（现有导入仅验扩展名，是反面教材）。
2. **越权**：管理员上传/停用接口须在 `RequireAuthentication` 后；用户读图须要查询会话；
   二维码绝不对未登录开放。
3. **响应头**：图片接口正确 `Content-Type` + `X-Content-Type-Options: nosniff` +
   合理 `Cache-Control`，防 MIME sniffing。
4. **日志安全**：绝不记录图片二进制或可恢复内容；只记方式/大小/类型/sha256/时间/管理员。
5. **DoS/体量**：单独 5MB 上限 + `MaxBytesReader`；multipart 内存/临时文件上限。
6. **真实二维码保护**：本轮只用生成的无效测试码；真实码由用户本人在管理员页上传，
   不入 Git、不入日志/截图/测试夹具。
7. **路径注入**：bytea 方案天然规避「用户可控文件名拼路径」。
8. **信息泄露**：普通用户端不得出现 DB ID/SHA/路径/管理员用户名等技术字段。

---

## 预计修改/新增文件

**后端**：
- `backend/migrations/0020_payment_qr_codes.sql`（新）
- `backend/internal/paymentqr/`（新包：handler + store + 图片魔数校验 + 测试）
- `backend/internal/api/router.go`（挂新路由）
- 可能 `backend/internal/query/handler.go`（用户端读图/状态接口，或新包内实现）

**前端**：
- `frontend/src/App.vue`（新增管理员二维码页、用户付款区、分流入口页、各列表筛选区、
  统一方框样式；工作量集中在此单文件）
- `frontend/src/api/client.ts`（可能补上传/读取封装）

**文档**：本日志持续追加。

---

## 明确回答第一停止点 12 项 + 补充 18–23

1. 当前付款流程：管理员按 CN 勾明细→选方式→核对手续费→`POST /api/admin/payments`
   直接落 `approved`；用户端只读订单/付款，无付款方式选择与二维码。
2. 推荐存储：**bytea 存 DB**（随 pg_dump 整库备份，迁移 Linux 即 pg_restore）。
3. 新表/API：`payment_qr_codes`；管理员 GET 状态/上传/停用/预览图，用户 GET 状态/读图。
4. 管理员页面：`/admin/payment-qr` 两张等宽克制蓝/绿卡片，含预览/替换/停用/更新时间/
   收起技术详情。
5. 用户页面：付款汇总 + 选择方式（蓝/绿等宽）+ 本次应付（放大只读）+ 收款二维码
   （可放大/空状态/提示）+ 付款历史。
6. 分块问题：见「阶段一.8」（管理员导航全局渲染、无分流首页、方框不统一、
   用户端无筛选等）。
7. 筛选矩阵：见「阶段七」（每列表独立筛选、用户端前端筛选、管理员后端筛选）。
8. 安全风险：见「安全风险清单」。
9. 预计修改文件：见「预计修改/新增文件」。
10. 是否需要迁移：**需要**，唯一新迁移 `0020_payment_qr_codes.sql`。
11. Git 状态：基线 HEAD 干净；工作区有 `M HANDOVER.md`、
    新日志、以及三个 XLSX 本地产物（`users-/order-items-/payments-*.xlsx`，
    **不得入 Git**）。本轮未改代码/未提交。
12. 是否使用子代理：否。
18. 登录入口分离信息架构：统一分流页 `/` → 普通用户 `/query` / 管理员 `/admin`。
19. 普通用户登录页：见上「普通用户登录页」。
20. 管理员登录页：见上「管理员登录页」（内联表单独立成页）。
21. 旧地址：`/query` 保留；`/` 改为分流页仍可用；`/admin/*` 未登录跳 `/admin`。
    建议保留旧地址不做硬跳转，避免收藏失效。
22. 预计新增/修改页面组件：全部集中在 `App.vue`（分流页、管理员二维码页、
    用户付款区、独立筛选区、统一方框样式）。
23. 与截图对齐边界：借鉴「居中大卡/大标题+副标题/圆角统一/留白/移动友好」；
    不照搬「选择店铺/手机号登录/记住密码/免登录天数」等本系统无对应的元素。

**第一停止点：未创建迁移、未改代码、未提交、未推送、未部署、未上传真实二维码。**

---

# 实现阶段

## 阶段一：基线复核与设计校正（完成）

### 只读复核结果（全部通过，无阻塞）
- `git status --short`：`M HANDOVER.md`；未跟踪 = 本日志、`2026-07-16-public-production-deployment-plan.md`、三个 XLSX 本地产物。工作区无代码改动。
- 分支 `main`，HEAD = origin/main = `c8e7df0d7143137b8f6374d2b376a42bb8530582`（干净基线）。
- 最高迁移仍为 `0019`；新迁移用 `0020_payment_qr_codes.sql`。
- `payments.screenshot_storage_path`：grep 确认 `.go` 代码零读写，仍未使用；本轮不复用。
- 鉴权中间件：管理员 `admin.RequireAuthentication`（middleware.go:14）注入 `Admin` 到 context，`admin.CurrentAdmin(ctx)` 取回（`Admin.ID string`）；普通用户 `query.userFromRequest`（handler.go:400）校验 `pjsk_query_session` HttpOnly cookie。
- 数据库访问层：`pgxpool`，store 接口 + `PostgresStore` 实现；测试为 `fakeStore`+`httptest` 单元测试，DB 集成测试用 `internal/testdb`（默认 `PJSK_RUN_DB_INTEGRATION_TESTS!=1` 全部 skip，`go test ./...` 不碰库）。
- `pgcrypto`：0001 `create extension if not exists pgcrypto`，`gen_random_uuid()` 可用。
- **`admins.id` 真实类型 = `uuid`**（0001:4）。故 `references admins(id)` 用 `uuid` 正确，非猜测。
- 迁移发现：`fs.ReadDir` + `sort.Strings`（字母序），`0020_` 排在 `0019_` 之后，顺序正确；`schema_migrations` 记录已应用版本。
- Caddy：`@backend path /api/* /health → 127.0.0.1:8080`，其余 `try_files {path} /index.html`（SPA 深链回退 OK），`request_body max_size 25MB`（>5MB，二维码上传由后端 `MaxBytesReader` 收紧到 5MiB）。
- `App.vue` 实际 3311 行，手写 pushState 路由，与日志一致。
- go 1.26.5；依赖无 `golang.org/x/image`（WebP 解码需另加依赖）。

### 设计校正（相对只读日志草案）
1. **历史模型改为不可变行 + 显式生命周期**：`enabled boolean` + `created_by/created_at`（上传即插新行）+ `disabled_by/disabled_at`（停用只改标志、保留历史行）。放弃草案 `updated_by/updated_at`（原地更新语义），因替换=插新行/停旧行，每行内容不可变，`created_by/created_at` 即「由谁在何时配置该码」，历史图片天然保留。「每方式最多一条生效」= `unique index (payment_method) where enabled`。替换事务：`pg_advisory_xact_lock(hashtext('payment_qr:'||method))` → 停旧生效行 → 插新生效行 → 提交；部分唯一索引作并发兜底；单事务保证「先停后插失败不丢当前码」。
2. **WebP 校验不新增依赖**：PNG→`png.DecodeConfig`，JPEG→`jpeg.DecodeConfig`，WebP→自实现 RIFF 容器校验（`RIFF`+长度+`WEBP`+合法 chunk `VP8 `/`VP8L`/`VP8X`）。存储 `mime_type` 由校验通过的真实格式决定，不信任客户端 Content-Type/文件名。`http.DetectContentType` 首 512 字节做第一道判定 + 结构解析做第二道。
3. 测试注入管理员：新增导出助手 `admin.ContextWithAdmin(ctx, Admin) context.Context`（与 `CurrentAdmin` 对称，仅构造 context 值，不改鉴权路径），使跨包 handler 成功路径可在无 DB 下单测。

### 阶段一结论
调查结论仍然有效，无阻塞问题。进入阶段二。

---

## 阶段二：迁移 0020 与后端数据层（完成）

### 新增迁移
`backend/migrations/0020_payment_qr_codes.sql`：表 `payment_qr_codes`。
- 列：`id uuid pk`、`payment_method text check in (alipay,wechat)`、`image_data bytea`、`mime_type text check in (image/png,image/jpeg,image/webp)`、`byte_size int check (>0 and <=5242880)`、`sha256 text check (len=64)`、`enabled bool`、`created_by uuid → admins(id) on delete set null`、`created_at timestamptz`、`disabled_by uuid → admins(id) on delete set null`、`disabled_at timestamptz`。
- 一致性 CHECK：`enabled=true` 时 disabled_* 必须为空；`enabled=false` 时 disabled_at 必须非空。
- `unique index ... (payment_method) where enabled`（每方式最多一条生效 + 并发兜底）。
- `index (payment_method, created_at desc)`（历史查询）。
- 未复用 `payments.screenshot_storage_path`。

### 新增后端包 `backend/internal/paymentqr/`
- `image.go`：`ValidateImage` 双重校验 —— `http.DetectContentType` 魔数 + 结构解析（PNG/JPEG 用 `image/png`、`image/jpeg` 的 `DecodeConfig` + 25MP 像素上限防解压炸弹；WebP 用自实现 RIFF/WEBP 容器 + `VP8 /VP8L/VP8X` chunk 校验，不新增依赖）。`mime_type` 由真实内容决定，不信任扩展名/客户端 Content-Type。5 MiB 上限。错误信息中文。
- `handler.go`：管理员 `AdminCollection`(GET 状态)/`AdminItem`(上传/停用/预览图路由)；用户 `UserAvailability`(GET 可用方式)/`UserImage`(GET 读图)。上传 `http.MaxBytesReader(5MiB+1)` + `ParseMultipartForm` + 单 `file` 字段 + `readAllLimited`。图片响应：正确 `Content-Type` + `X-Content-Type-Options: nosniff` + `Cache-Control: private, max-age=0, must-revalidate` + `ETag=sha256`（支持 `If-None-Match` → 304）。日志只记 method/mime/size/sha256/adminID/结果，绝不记二进制。管理员状态含技术字段；用户可用性只含 `payment_method/available`。
- `store.go`：`PostgresStore`。`Upload` 事务 = `pg_advisory_xact_lock(hashtext('payment_qr:'+method))` → 停旧生效行 → 插新生效行 → 提交；`Disable` 同锁，改 enabled=false 保留历史，无生效行时返回 `ErrNotConfigured`。`AdminStatuses`/`UserAvailability` 不读 `image_data`。
- `util.go`：`readAllLimited`。
- 测试：`image_test.go`（PNG/JPEG/WebP 接受；空/超限/SVG/HTML/GIF/PDF/EXE/ELF/纯文本/伪装 PNG/仅签名/像素炸弹 拒绝；WebP 坏容器拒绝；中文错误）；`handler_test.go`（未鉴权上传 401、非法 method 404、空文件/伪装/超限 400、有效 PNG 200 且 store 收到正确 method/mime/adminID/sha/size、停用 200、停用无记录 404、管理员状态含 sha256、用户可用性不含技术字段、图片响应头/ETag/304/未配置 404/非法 method 404）；`store_integration_test.go`（替换后单一生效+历史保留、停用保留历史、8 并发上传仍单一生效、状态/可用性）。

### 复用性最小改动
- `admin.ContextWithAdmin`（middleware.go）：与 `CurrentAdmin` 对称，供跨包 handler 单测注入管理员。
- `query.Handler.RequireSession`（handler.go）：复用查询会话校验，网关用户端 QR 接口。
- `api/router.go`：挂载 4 条 QR 路由（2 管理员 RequireAuthentication + 2 用户 RequireSession）。

### 测试结果
- `gofmt -l .`：干净。
- `go build ./...`：通过。
- `go vet ./...`：通过。
- `go test ./...`（无 DB，集成自动 skip）：全部 ok，含新 `paymentqr` 与 `api` QR 路由鉴权测试。
- `PJSK_RUN_DB_INTEGRATION_TESTS=1 go test ./internal/paymentqr/`（本机临时隔离库 `pjsk_integration_test_paymentqr_*`，用后即删，绝不触碰生产 `pjsk`）：全部 PASS。迁移 0020 在 0019 后按序应用；8 并发上传后 `enabled=1/total=8`，验证「不会出现两个生效二维码」「历史保留」。
- `git diff --check`：干净。迁移最高 `0020`，两个历史 `0005` 未动。

### 安全措施落实（对照阶段二要求）
上传 ≤5MiB（MaxBytesReader）、单 file 字段、拒空、不信扩展名/文件名、内容级双重格式识别、只允许 PNG/JPEG/WebP、显式拒绝 SVG/GIF/HTML/PDF/EXE/ELF/未知、图片响应正确 Content-Type + nosniff + Cache-Control + ETag/304、SHA 不进用户 JSON（仅 ETag header）、日志无二进制/Base64。管理员状态不返回 `image_data`。替换事务原子（先停后插同事务，失败回滚不丢当前码）。

### 未触碰真实业务数据
仅用程序生成的测试图片与临时隔离测试库；未上传任何真实二维码；三个 XLSX 未暂存。

### 阶段二结论
后端 MVP 完成且验证通过，无阻塞。进入阶段三（管理员收款二维码页面）。

---

## 阶段三：管理员收款二维码页面（完成）

### 前端改动（均在 `frontend/src/App.vue` + `api/client.ts` + `style.css`）
- 路由：新增 routeName `admin-payment-qr`（`RouteName` 类型 + `routeFromPath('/admin/payment-qr')`），导航栏与 admin-actions 增加「收款二维码」入口，`handleRouteEntered` 进入时 `loadPaymentQRStatuses()`。
- `client.ts`：`PaymentQRMethod/PaymentQRAdminStatus/PaymentQRAvailability` 类型 + `getPaymentQRAdminStatuses/uploadPaymentQR/disablePaymentQR/getPaymentQRAvailability` 助手。
- 状态：`paymentQRStatuses/paymentQRLoading/paymentQRMessage/paymentQRReloadKey` + per-method `qrSelectedFile/qrPreviewURL/qrUploading/qrDisabling` + `paymentQRStatusByMethod` 计算。
- 函数：`loadPaymentQRStatuses`、`adminQRImageURL`（用后端图片接口 + sha/reloadKey 破缓存，**不把图片转 Base64 进前端状态**）、`onQRFileChange`（客户端校验 MIME 白名单 + 5MiB，生成本地 objectURL 预览）、`clearQRSelection`（revoke objectURL）、`uploadPaymentQRImage`（已配置时二次确认再替换）、`disablePaymentQRImage`（二次确认）。
- 模板：`admin-payment-qr` 页两张 `v-for="method in qrMethods"` 同结构卡片（支付宝克制蓝 / 微信克制绿），标题状态居中；已配置显示后端图片预览 + 更新时间，未配置显示空状态；上传区显示本地预览 + 文件名/格式/大小；停用为红色危险按钮；技术详情（格式/大小/更新管理员/更新时间/SHA-256）放入**默认收起**的 `<details class="technical-panel qr-technical">`。
- `style.css`：`qr-admin-grid`（桌面 2 列，≤560px 单列）+ `qr-card`/`qr-card--alipay/--wechat` + 预览/上传/技术详情样式；修复 `<input type=file>` 内在最小宽度在 320px 撑破卡片的问题（`.qr-file-picker input { width/min-width/max-width }` + `.qr-card/.qr-card__upload { min-width:0 }`）。

### 测试与验证
- `vue-tsc -b`：通过；`pnpm run build`：通过。
- 前端契约测试 `tests/payment-qr-admin.test.mjs`（8 项）+ 既有 `query-user-view`（4 项）= 11 项全通过；`package.json` test 脚本改为 `node --test "tests/**/*.test.mjs"`。
- **浏览器验收（临时隔离栈：新后端 :8090 + 独立库 `pjsk_qr_dev` + Vite 代理，未触碰生产 :8080/`pjsk`）**：
  - 后端 curl E2E：管理员登录→状态（both 未配置）→上传支付宝测试 PNG→状态变已配置（mime/size/sha/updated_by=devadmin）；图片接口返回 `200 image/png` + `nosniff` + `ETag` + `Cache-Control: private, max-age=0, must-revalidate`；伪装 HTML（.png 名）被拒 `400` 中文提示；用户登录→可用性（alipay 可用/wechat 否，无技术字段）→用户图片 `200`；未登录用户图片 `401`。
  - 浏览器 UI：devadmin 登录后 `/admin/payment-qr` 两卡渲染正确（蓝/绿、已配置预览+更新时间、未配置空状态）；技术详情默认收起（fresh load `open=false`）；Console 无错误；320px 无页面级横向滚动（`scrollWidth==clientWidth==320`），两卡纵向堆叠、危险按钮全宽。
  - 文件选择器上传/替换/停用的完整点击流因浏览器工具无法驱动系统文件选择框，改由 curl E2E 全量覆盖（上传/替换/停用/图片服务），后端集成测试另验单一生效+历史保留+并发。
- `git diff --check`：干净。

### 未触碰真实业务数据
浏览器验证使用临时隔离库与程序生成测试 PNG；未触碰生产库；未上传真实二维码；三个 XLSX 未暂存；未提交。

### 阶段三结论
管理员收款二维码页面完成并通过验证。进入阶段四（普通用户付款二维码区域）。

---

## 阶段四：普通用户付款二维码区域（完成）

### 金额业务口径核对（阶段四要求的风险点）
- 后端事实：`payments.payable_amount = submitted_amount(本金) + fee_amount`；`fee_amount` 仅微信有（0.1% 向上取整，整数分），支付宝为 0（`payments/handler.go:calculateFee`）。用户 `/api/query/orders` 的 `remaining_amount` 由 `order_items` 金额（**本金口径**，不含手续费）汇总得出。
- 结论：现有业务口径是「管理员按本金勾选明细录入付款，微信手续费在管理员端单独计算并计入实付」，**用户端展示的未付金额是本金**。是否要求微信用户扫码时额外支付手续费属**业务政策问题，代码与日志无法唯一确定**。
- 处置（遵循「不要猜测、不改金额算法」）：用户「本次应付」严格显示后端 `remaining_amount`（本金），**不在前端重算任何手续费**，并加提示「付款完成不代表系统已自动确认，最终以管理员录入结果为准」。此口径歧义记录为**待业务确认事项**，本轮不改。

### 前端改动（`App.vue` + `client.ts` + `style.css`）
- `client.ts`：`getPaymentQRAvailability` + `PaymentQRAvailability` 类型（阶段三已加）。
- 状态：`queryQRAvailability/queryQRLoading/queryQRError/queryQRMethod/queryQRZoom/queryQRReloadKey` + `queryQRAvailableMethods` 计算。
- 函数：`loadQueryQRAvailability`（登录后随订单加载；401 走会话失效流程；默认选中第一个可用方式）、`selectQueryQRMethod`、`queryQRImageURL`（用 `/api/query/payment-qr/{method}/image`，无任何技术标识）、`onQueryQRImageError`（中文错误）、`openQueryQRZoom`/`closeQueryQRZoom`；`logoutQuery` 清理 QR 状态。
- 模板（`/query` 登录后，独立卡片、不与订单/历史混在一个面板）：
  - 【付款汇总】标题 + 4 个等尺寸居中方框（总金额/共件数/已付金额/未付金额）；移除原先把 CN、订单数挤在一起的 6 格。CN 移到账户信息区（登录后面板头「当前登录 CN：xxx」），订单数移到「订单明细 / 共 N 个订单」标题旁。
  - 【选择付款方式】只渲染后端已配置且启用的方式；支付宝蓝 / 微信绿等宽按钮；默认选中第一个；全部未配置显示空状态。
  - 【本次应付】放大加粗红色显示未付金额，**只读无输入框**，附核对提示。
  - 【收款二维码】按选中方式显示后端图片，完整不拉伸，`cursor: zoom-in` 点击放大（`.qr-zoom-overlay` 灯箱），切换方式更新图片，加载失败中文提示，未配置空状态「管理员暂未配置该付款方式的收款二维码。」
  - 本轮**未加**「我已付款」、未上传截图、未创建待审核付款、扫码不改状态。

### 测试与浏览器验收
- `vue-tsc -b` / `pnpm run build` 通过；前端契约测试 18 项全过（新增 `payment-qr-user.test.mjs` 7 项，含「无技术字段泄露」「本次应付只读无输入」「用户端不重算手续费」）。
- **浏览器实测**（隔离测试管理员上传支付宝测试码后，以隔离测试 CN + 测试查询码登录 /query）：付款汇总 4 格（总金额 60.00 / 共 2 件 / 已付 / 未付）；仅显示「支付宝」（微信未配置，不出现）；本次应付 60.00 放大红色；收款二维码由 `/api/query/payment-qr/alipay/image?v=0` 成功加载（`complete && naturalWidth>0`）；点击弹出放大灯箱并可关闭；CN 显示在账户区；订单明细/付款历史正常；**`main` 文本中无 sha256/byte_size/updated_by/image/png/订单号/SKU/项目码等技术字段**；320px 无页面级横向滚动，汇总 4 格自适应 2 列、文字居中；Console 无错误。

### 未触碰真实业务数据
仍使用临时隔离库与测试图片；未提交。

### 阶段四结论
普通用户付款二维码区域完成；微信手续费口径歧义已记录为待业务确认，未改金额算法。进入阶段五（登录入口与导航分离，按用户 2026-07-16 更新指令）。

---

## 阶段五：登录入口与导航分离（完成，按 2026-07-16 更新指令）

### 品牌与参考边界
全系统主页与两类登录页统一使用本系统名称 **「PJSK 谷子系统」**。用户提供的登录截图与参考站点 https://rensheet.top/ **仅作布局/风格参考**，不复制其品牌文字；「音游窝」等参考来源文字不出现在任何界面（契约测试断言 App.vue 不含「音游窝」）。

### 路由与信息架构
- `/` 统一分流主页（品牌 + 「请选择登录入口」+ 两张等宽等高入口卡片：普通用户入口 / 管理员入口，各带说明文案，居中，桌面并排、≤560px 纵向）。移除原开发者总览（模块状态/接口清单/Streamlit 命令）。
- `/query` 普通用户登录及业务（顶部加品牌头「PJSK 谷子系统 / 用户服务入口」+ 返回入口选择；保留 CN/查询码/登录/忘记查询码/首次设置/邮箱找回，业务逻辑与 401/限流/会话/找回全部未改）。
- `/admin` 管理员登录页（品牌 + 「管理员入口」+ 用户名/密码/登录 + 返回入口选择；复用原 `login()`，认证/限流/Cookie/审计逻辑未改）。
- `/admin/*` 管理员业务页（未改）。

### 导航隔离
- `isAdminRoute` 重定义为「排除 home/query/admin-login」，`showAdminNav = admin!==null && isAdminRoute`。管理员导航 `<nav v-if="showAdminNav">` 仅在**已登录管理员 + 位于管理员业务页**时渲染；分流主页、用户页、两类登录页均不显示任何管理员导航（Excel 导入/导入历史/用户管理/订单/付款/收款二维码）。顶栏也仅在管理员业务页显示。导航移除指向 `/` 的「总览」按钮。

### 深链兼容
- 未登录访问 `/admin/orders`、`/admin/users`、`/admin/payment-qr` 等 → `handleRouteEntered` 记录 `pendingAdminTarget` 并 `navigate('/admin')` 进入登录页。
- 登录成功后 `navigate(pendingAdminTarget || '/admin/imports')`，**优先返回原目标页**。
- 已登录管理员访问 `/admin` → 自动跳转到目标或默认管理员首页。
- 退出登录 → `navigate('/admin')` 回到管理员登录页。
- 旧地址：`/query` 不变；`/` 由开发者总览改为分流主页（仍可用，旧收藏不失效）；`/admin/*` 深链保留、未登录安全跳登录后回原页。

### 移除的未使用状态
删除仅服务于开发者总览的 `activeView`、`readyCount`、`queuedCount`（避免 `noUnusedLocals` 报错）。

### 测试与浏览器验收
- `vue-tsc -b` / `pnpm run build` 通过；前端契约测试 25 项全过（新增 `login-entry.test.mjs` 7 项：路由/品牌无音游窝/两卡无开发者信息/导航隔离/深链重定向与回跳/两登录页保留原流程/入口 CSS 320px 单列）；更新 `query-user-view.test.mjs` 结束标记（原 metrics 块已移除）。
- **浏览器实测**（临时隔离栈）：
  - `/`：品牌「PJSK 谷子系统」+「请选择登录入口」+ 两张等宽卡片；**无管理员导航、无顶栏、无开发者信息（运行指标/Streamlit/可用模块/后端接口）、无「音游窝」**；320px 无横向滚动、两卡等宽纵向堆叠。
  - `/query`：品牌 +「用户服务入口」+ 返回入口选择；无管理员导航；无「音游窝」。
  - `/admin`：管理员登录表单 +「管理员入口」；无管理员导航。
  - 已登录管理员访问 `/admin` → 自动跳 `/admin/imports`。
  - 退出 → `/admin` 登录页（无导航）。
  - **未登录深链 `/admin/orders` → 跳 `/admin` 登录 → 登录成功 → 回到 `/admin/orders`**（admin 导航与身份出现）。
  - 管理员导航仅 6 个业务标签（无「总览」）。
  - Console 无错误。
  - （截图工具本会话间歇超时，改用页面 DOM 断言取证，结论确定。）

### 未触碰真实业务数据 / 未改认证安全逻辑
仅前端信息架构与导航可见性调整；管理员/用户认证、限流、会话、找回、后端接口路径全部未改。未提交。

### 阶段五结论
登录入口与导航分离完成并通过验证。进入阶段六（方框/分块/局部视觉统一）。

---

## 阶段六：方框、分块与局部视觉统一（完成）

按「仅整理本轮涉及页面、不做全站无边界重构」执行。大部分方框一致性在阶段三～五已随新组件建立，本阶段做聚焦统一与 320px 收口：

- **信息框统一**：`.metric-tile` 增加 `align-items:center; text-align:center`，配合原有 `min-height:92px` 与 `justify-content:space-between`，使同类数值框等高、标题与数值水平垂直居中（回应「方框大小一致、文字居中完美嵌合」）；`.metric-tile strong` 保留 `overflow-wrap:anywhere`，长金额换行不溢出、不截断。
- **等尺寸卡片**：入口卡 `.entry-choice`（min-height 180 + grid 等宽）、QR 卡（grid `align-items:stretch`）、QR 预览框 `.qr-card__preview`（min-height 180）、付款方式按钮 `.query-method-button`（`flex:1` + min-height 46）均等宽/等高。
- **320px 收口（关键修复）**：将共享规则 `.login-form input, .file-picker input` 增加 `min-width:0; max-width:100%`，根治「裸 `<input type=file>` 内在最小宽度撑破窄屏」——修复了**既有 Excel 导入预览页**在 320px 的 18px 横向溢出（此前非本轮引入，但顺手在共享规则层面解决，未改导入逻辑）。
- **技术信息**：收款二维码技术详情用默认收起 `<details>`；普通用户端不渲染任何技术字段（阶段三/四已落实）。
- **状态区分**：加载（`.muted` 文本）/ 空（带边框的 `.qr-empty` 盒）/ 错误（橙色 `.inline-alert`）样式区分。
- **未改坏**：Excel 导入预览、订单表格、付款明细展开、移动端订单卡片逻辑均未改，仅样式层面统一。

### 测试与浏览器验收
- `vue-tsc -b` / `pnpm run build` 通过；前端契约测试 29 项全过（新增 `box-consistency.test.mjs` 4 项）。
- 浏览器实测（320px）：`/admin/imports`、`/admin/payments`、`/admin/users`、`/admin/orders`、`/admin/payment-qr` **全部无页面级横向滚动**（导入页由溢出 338 收敛到 320）；宽表仍保留自身 `table-scroll` 局部横向滚动。
- `git diff --check` 干净。

### 阶段六结论
本轮涉及页面的方框/分块/居中/320px 统一完成。进入阶段七（独立筛选）。

---

## 阶段七：独立筛选（完成 P1；P2 记录为后续）

按「先做高价值低风险 P1」执行。

### 已实现（第一优先级）
- **普通用户订单筛选**（前端，单用户数据小）：分类 / 角色 / 系列 / 付款状态。独立状态 `queryOrderFilters`，`filteredQueryOrders` 计算**新建对象、不改原数据**，只影响订单明细列表；订单标题旁显示「共 N 个订单，当前筛选出 M 个」；带「清空筛选」与筛选空状态；明确注明「订单筛选只影响下方订单明细，不改变上方付款汇总合计口径」。
- **普通用户付款历史筛选**（前端）：付款方式 / 状态 / 时间起止。独立状态 `queryPaymentFilters`，`filteredQueryPayments` 计算；与订单筛选互不影响；筛选栏仅在有付款记录时显示；带清空与筛选空状态。
- **管理员未付明细筛选**（前端，显示层）：分类 / 角色 / 系列（CN 已是加载键，作为固定上下文显示）。`filteredCnPaymentItems` 只改**显示行**；`selectedPaymentItemIds` 与 `paymentAmounts` 仍作用于完整 `cnPayment.items`——**已勾选但被筛选隐藏的明细仍计入本次付款**，注明该口径；带清空与筛选空状态。未改任何付款选择/金额/保存逻辑。
- **管理员付款记录**：本就有独立筛选块（CN / 付款方式 / 状态 / 付款时间起止）+「重置」（= 清空并重查）；本轮确认其为独立分块，满足「清空筛选」要求。

### 独立性与口径
每个列表的筛选器只控制本列表；用户订单筛选不影响付款历史，反之亦然；管理员未付明细筛选不影响勾选与合计。付款汇总/合计始终为全部数据口径，界面已注明。

### 后续任务（第二优先级，本轮不做，避免范围失控）
- 管理员订单金额范围筛选；
- 管理员付款记录金额范围筛选；
- 管理员用户列表更多筛选（是否设查询码/是否绑邮箱/是否有订单/是否有未付）；
- 导入历史高级筛选（文件名/Sheet/时间/状态/有无错误）；
- 若后续普通用户单 CN 数据量显著增大，再评估是否需要后端分页/筛选。

### 测试与浏览器验收
- `vue-tsc -b` / `pnpm run build` 通过；前端契约测试 35 项全过（新增 `independent-filters.test.mjs` 6 项）。
- **浏览器实测**：
  - 用户订单：分类下拉自动列出 吧唧/立牌/色纸；选「色纸」→ 仅剩 1 条且标题显示「当前筛选出 1 个」；「清空筛选」恢复 3 条、下拉复位、无横向滚动。
  - 用户付款历史：有付款时筛选栏出现；状态筛「已撤销」→ 命中 0 显示筛选空状态；清空恢复；**订单列表不受影响（独立）**。
  - 管理员未付明细：加载 CN测试01（2 条未付：吧唧/立牌）；勾选「初音未来吧唧」后「1 条明细」；按分类筛「立牌」隐藏该行后，**选中计数仍为「1 条明细」**（选择随筛选保持），验证显示层筛选不影响勾选与合计。

### 未触碰真实业务数据
仅前端过滤 + 临时隔离库测试数据；未提交。

### 阶段七结论
P1 独立筛选完成并通过验证；P2 记录为后续任务。进入最终总验收。

---

## 最终总验收（完成，未提交/未推送/未部署/未执行正式迁移/未上传真实二维码）

### 后端
- `go fmt ./...` 干净；`go build ./...` 通过；`go vet ./...` 通过；`go test ./...` 全绿。
- `PJSK_RUN_DB_INTEGRATION_TESTS=1 go test ./...`（本机临时隔离库）：18 个包全部 ok，无 FAIL/panic。含二维码专项（格式伪装/大小/空文件/越权/替换单一生效/停用/历史保留）、鉴权与路由、迁移顺序、并发替换（8 并发→单一生效）。

### 前端
- `vue-tsc -b` 通过；`pnpm run build` 通过；`pnpm run test` 35 项全过（登录入口、管理员二维码页、用户付款区、独立筛选、方框一致性、320px 契约、技术字段不泄露）。

### 仓库
- `git diff --check` 干净（仅 style.css 的 CRLF→LF 归一化提示，由 .gitattributes `*.css eol=lf` 处理，属正常）。
- 无二进制图片/Base64/data:image 进入仓库；无密码/Token/Cookie/真实二维码进入仓库（日志已把测试凭据泛化）。
- 三个 XLSX 本地产物未暂存、保持未跟踪。
- 历史开发日志 42 个全部完好，未删除/改名；本轮仅追加今日日志。
- 两个历史 `0005` 未改，新迁移唯一 `0020_payment_qr_codes.sql`。

### 修改/新增文件清单（diff：9 改 + 新增包/迁移/测试，约 +1232/-46）
改：`HANDOVER.md`(基线既有改动，非本轮)、`backend/internal/admin/middleware.go`、`backend/internal/api/router.go`、`backend/internal/query/handler.go`、`frontend/package.json`、`frontend/src/App.vue`、`frontend/src/api/client.ts`、`frontend/src/style.css`、`frontend/tests/query-user-view.test.mjs`。
新：`backend/migrations/0020_payment_qr_codes.sql`、`backend/internal/paymentqr/{handler,image,store,util,handler_test,image_test,store_integration_test}.go`、`backend/internal/api/payment_qr_routes_test.go`、`frontend/tests/{payment-qr-admin,payment-qr-user,login-entry,box-consistency,independent-filters}.test.mjs`、本日志。

### 数据库迁移影响
新增一张表 `payment_qr_codes`（bytea 存图 + 生命周期元数据 + 部分唯一索引 + 两个普通索引）。正式库尚未执行（仅在本机临时隔离库验证）。执行时按现有启动自动迁移机制在 0019 之后应用 0020，向后兼容、不改动既有表。

### 是否触碰真实业务数据 / 是否使用子代理
否 / 否。全程单会话，仅用程序生成的无效测试图片与临时隔离库（用后即删）。

### 未完成事项 / 已知风险
1. **微信手续费口径歧义**（待业务确认）：用户「本次应付」显示未付本金，微信手续费在管理员端单独计入；未在用户端重算，未改金额算法。
2. **筛选 P2 后续**：管理员订单/付款金额范围、管理员用户更多筛选、导入历史高级筛选。
3. **登录深链返回**：已实现返回原目标页；若未来路由复杂化需回归测试。
4. 浏览器截图工具本会话间歇超时，验收改用页面 DOM 断言（结论确定）；文件选择框上传流由 curl E2E + 集成测试覆盖。
5. 临时隔离栈（:8090 后端、:5173 Vite、`pjsk_qr_dev` 库）为验收用，会话末需停止并 drop（因安全分类器临时不可用，清理命令待补执行；与生产 :8080/`pjsk` 完全隔离，无副作用）。

### 人工验收清单（建议在正式栈由用户执行）
- [ ] 正式后端应用迁移 0020（`payment_qr_codes` 建表）。
- [ ] 管理员 `/admin/payment-qr` 上传**真实**支付宝/微信收款码（用户本人操作，不经对话/自动化）。
- [ ] 预览、替换（二次确认）、停用（二次确认）、更新时间/更新管理员正确。
- [ ] 普通用户登录后：付款汇总四格、选择方式、本次应付、扫码二维码、点击放大、未配置空状态、无技术字段。
- [ ] 分流主页 / 用户登录 / 管理员登录三页品牌「PJSK 谷子系统」正确，导航隔离，深链跳转与回跳正常。
- [ ] 桌面 / 768px / 320px 三档，Console 无红色错误。
- [ ] 用户订单/付款历史筛选与清空、管理员未付明细筛选（勾选保持）正常。

### 是否具备进入「正式迁移、提交和部署」阶段的条件
具备。代码、测试、隔离库验收均通过，未触碰真实数据。**按约束停在此处，等待你明确允许后再提交/推送/部署/执行正式迁移/上传真实二维码。**

---

# 收尾、提交、推送与正式部署

## 部署阶段一：清理临时测试环境（完成）

只读确认后停止，均为本轮临时进程，未动生产：
- 端口 5173 → PID 13068 = `node D:\pjsk\frontend\node_modules\...\vite.js`（临时 Vite），已 `Stop-Process`。
- 端口 8090 → PID 21332 = `%LOCALAPPDATA%\Temp\pjsk-qr-dev.exe`（临时后端），已 `Stop-Process`。
- 复查：5173/8090 均不再监听。
- 数据库只读核对（`D:\PostgreSQL\18\bin\psql.exe`）：`pjsk`（正式）与 `pjsk_qr_dev`（临时）并存、名称完全不同、`pjsk_qr_dev` 活动连接 0；`drop database if exists pjsk_qr_dev` 成功，复查仅剩 `pjsk`。**未触碰 `pjsk`。**
- 生产服务不受影响：8080 后端(PID 21248)、8081 Caddy(PID 1176)、5432 PostgreSQL(PID 9108) 均在；`http://127.0.0.1:8081/health` = `{"status":"ok","database":"connected"}`。

---

## 新增停止点：分模块人工预览与用户验收（预览环境已就绪，等待用户验收）

**当前状态：尚未提交、尚未推送、尚未执行正式迁移。等待用户本人浏览器验收。**

### 预览环境（隔离，未连接正式库）
- 临时数据库：`pjsk_qr_dev`（≠ `pjsk`，未连接/未修改正式库）；临时后端 `127.0.0.1:8090`；前端 `http://127.0.0.1:5173/`。
- 测试管理员：`qa_admin` / `qa-admin-pass-1`（仅临时库）。
- 测试普通用户：CN `测试CN01` / 查询码 `qa-user-code-1`（仅临时库）。
- 测试数据：项目「验收测试团（测试数据）」；4 件明细（吧唧/色纸/立牌/亚克力，4 角色/4 系列 MK-01~04）；总额 145，已付 25（色纸），未付 120；1 条已交肾（支付宝 25）+ 1 条已撤销（微信 21）付款记录。
- 收款二维码：**支付宝配置了程序生成的 240×240 无效测试码**（非真实收款码），**微信未配置**（用于查看空状态）。

### Codex 自动验收（浏览器 DOM 检查；截图工具本会话超时不可用，已改用 DOM 断言，未伪造截图）
9 个页面全部核对通过、无页面级横向滚动、Console 无红色错误：
1. `/`：品牌「PJSK 谷子系统」+「请选择登录入口」+ 两张等宽入口卡；无管理员导航/顶栏/开发者信息/「音游窝」。
2. `/query` 未登录：品牌 +「用户服务入口」+ CN/查询码/查询订单 + 首次设置查询码；邮箱找回显示「暂不可用（邮件服务未配置）」（预览环境邮件禁用，属既有正确行为）；无管理员导航。
3. `/query` 登录后：付款汇总 4 等尺寸框（总额 145 / 件数 5 / 已付 25 / 未付 120）；本次应付 120.00；仅「支付宝」按钮（微信未配置不显示）；支付宝测试码由 `/api/query/payment-qr/alipay/image` 加载成功；订单筛选栏 + 付款历史筛选栏各自独立；**无 sha256/byte_size/updated_by/订单号/SKU/项目码等技术字段泄露**。
4. `/admin` 未登录：品牌 +「管理员入口」+ 用户名/密码/登录；无管理员导航。
5–9. `/admin/imports`(Excel 导入)、`/admin/users`(用户管理)、`/admin/orders`(订单查询)、`/admin/payments`(录入付款+付款记录)、`/admin/payment-qr`(支付宝已配置+图片加载、微信未配置空状态、两卡等宽、技术详情默认收起) 各自独立成页、导航正常。
- 管理员付款页加载 `测试CN01` 未付明细 3 条（初音未来吧唧/瑞希亚克力/奏立牌，色纸已付），未付明细筛选栏在位。

### 声明
未上传真实二维码；未触碰真实业务数据；未使用子代理；未提交/推送/迁移。等待用户明确答复「页面可以，继续提交」/「有问题，需要修改」后再继续。

## 提交前隐私脱敏说明（2026-07-18）

本日志在首次纳入 Git 前进行了最小隐私脱敏：仅将包含本机用户名的用户目录改写为 `%USERPROFILE%`、`%LOCALAPPDATA%` 或 `%APPDATA%` 形式，并将完整本地数据库连接串改写为 `<LOCAL_ISOLATED_DATABASE_URL>`。功能结论、测试结果、数据库归属、端口、迁移编号、执行顺序和安全边界均未改变。
