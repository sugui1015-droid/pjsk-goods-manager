# 2026-07-17 门户分流路由与付款方式完善

本轮：把「登录后仍堆在单页」的结构改为三级门户（角色入口 → 模块门户 → 具体功能页），修复
普通用户付款只显示支付宝的问题（补微信 + 手续费联动 + 二维码随方式切换），并做全站文字居中与
响应式修正。允许改前端代码/前端路由/与付款方式直接相关的后端接口；不改真实业务数据、不执行迁移、
不提交推送、不使用子代理。

- 分支 main；HEAD = origin/main = `c8e7df0d7143137b8f6374d2b376a42bb8530582`。
- 工作区含上一轮「静态收款二维码」未提交改动（QR 后端/前端/迁移 0020/测试/日志）+ 三个 XLSX（不入 Git）。
- 是否使用子代理：否。

---

## 阶段一：只读调查与路由设计

### 当前页面结构问题
1. **单文件承载**：`frontend/src/App.vue` 现 3818 行，`<script setup>` 内约 200 个 ref + 数十函数 + 全部管理员/用户模板堆在一起，靠大量 `v-if(routeName)` 硬切换（正是指令禁止的"混在同一模板靠条件判断硬切换"）。
2. **登录后仍单页堆叠**：管理员登录后 `/admin/imports` 直接出现 Excel 文件选择框；普通用户 `/query` 登录后把付款汇总/方式/二维码/订单/付款历史/账号安全全部纵向堆在一页。无"模块门户"层。
3. **重复入口**：管理员业务页顶部有一排 `nav.tabs`（6 个），正文"管理员导入中心"卡片里又有一排相同的 action 按钮 —— 顶部一遍、正文一遍，重复。
4. **付款方式不完整**：普通用户付款区只 `v-for` 渲染 `queryQRAvailableMethods`（**仅已配置**的方式），微信未配置时根本不出现微信按钮 —— 用户看到"只有支付宝"。且用户端未显示手续费/本次应付拆分。
5. **文字对齐不统一**：标题/按钮/说明/导航/表格等未统一居中；窄屏导航被挤成逐字换行（"收款二维码"竖排）。
6. **无 vue-router**：手写 `pushState` + `routeFromPath`。经查 `vue-router` 未安装且本地 pnpm store 无缓存（离线不可 `add`）——本轮**沿用并扩展手写路由**，不引入新依赖（降低风险）。

### 现有关键规则（复核，必须沿用）
- **微信手续费**：后端 `payments/handler.go:calculateFee` = `feeCents = (baseCents + 999) / 1000`（整数分向上取整，0.1%）；前端管理员录入侧已有完全一致的 `Math.floor((baseCents + 999) / 1000)`。本轮用户付款中心**复用同一公式**，以分为单位，禁止浮点四舍五入。支付宝手续费恒 0。
- 付款方式字段：`alipay/wechat/bank/cash/other`（本轮二维码/用户付款只涉及 alipay/wechat）。
- 二维码接口：管理员 `/api/admin/payment-qr(/{method}/image|disable)`；用户 `/api/query/payment-qr`、`/api/query/payment-qr/{method}/image`。用户可用性接口返回 `{payment_method, available}`。
- 用户端 DTO 已裁剪技术字段（订单号/项目/SKU/import/SHA/管理员），必须继续保持。

### 参考站点
`https://rensheet.top/` 仅借鉴其**信息架构原则**（入口清楚 → 模块卡片化 → 先进模块再进具体功能），
不复制其品牌、文字、图片、配色、代码或页面内容；系统名统一 `PJSK 谷子系统`，不得出现"音游窝"。

### 准备建立的路由层级（手写路由扩展）
**第一级 角色入口**
- `/` 角色分流（普通用户入口 / 管理员入口）——沿用现有分流页。

**管理员（登录后三级）**
- `/admin`：未登录=管理员登录页；已登录=**管理员模块门户**（仅模块卡片 + 顶部身份/状态栏，无任何业务表单/表格）。
- `/admin/data` 数据导入中心（卡片：Excel 导入 / 导入历史；**此层不出现文件选择框**）
  - `/admin/data/import` Excel 导入预览（真正的上传页）
  - `/admin/data/history`、`/admin/data/history/{id}` 导入历史/详情
- `/admin/orders`（订单管理，含订单查询与筛选）、`/admin/orders/{id}` 订单详情
- `/admin/users`（用户与账号）、`/admin/users/{id}` 用户详情
- `/admin/finance` 收付款管理（卡片：付款记录 / 收款二维码）
  - `/admin/finance/payments`、`/admin/finance/payments/{id}` 付款记录+录入/详情
  - `/admin/finance/qr-codes` 收款二维码管理
- 系统状态：不单独造空页，放门户顶部小型状态栏（系统名/管理员/后端在线/刷新/退出）。

**普通用户（登录后三级）**
- `/query`：未登录=用户登录页；已登录=**用户模块门户**（模块卡片 + 顶部 CN/退出/简短付款摘要）。
- `/query/orders` 我的订单（汇总+明细+分类/角色/系列/状态筛选）
- `/query/payment` 付款中心（总/件/已付/未付 + 方式选择 + 本金/手续费/本次应付 + 二维码）
- `/query/payments` 付款记录（历史+可展开明细，无技术标识）
- `/query/security` 账户安全（改查询码/恢复邮箱/邮箱验证）

**旧地址兼容（重定向到新功能页，API 不变）**
- `/admin/imports`→`/admin/data/import`；`/admin/imports/history`→`/admin/data/history`；`/admin/imports/{id}`→`/admin/data/history/{id}`
- `/admin/payments`→`/admin/finance/payments`；`/admin/payments/{id}`→`/admin/finance/payments/{id}`
- `/admin/payment-qr`→`/admin/finance/qr-codes`
- `/admin/orders`、`/admin/users` 路径保留（现作为模块页，兼容旧书签）。
- 未登录访问受保护页 → 跳对应角色登录页（管理员→`/admin`，用户→`/query`），不串入另一角色入口。

### 模块划分名称
- 管理员：**数据导入中心 / 订单管理 / 用户与账号 / 收付款管理**（+ 顶部系统状态栏）。
- 普通用户：**我的订单 / 付款中心 / 付款记录 / 账户安全**。

### 组件化（稳妥拆分，不无边界重写）
新增**表现型（prop 驱动）组件**，复用于门户与付款区，避免解耦巨型状态：
- `ModuleCard.vue`（模块卡片：名称/说明/数量或状态/进入按钮，等高居中）
- `PortalStatusBar.vue`（顶部系统名/身份/后端状态/刷新/退出）
- `PaymentMethodSelect.vue`（支付宝蓝/微信绿等宽按钮）
- `AmountSummary.vue`（本金/手续费/本次应付三等尺寸框）
- `QrDisplay.vue`（二维码 + 标题 + 空状态 + 点击放大）
业务态与逻辑仍集中在 App.vue（作为路由壳 + 状态源），通过 props/emit 传给上述表现型组件；
路由/权限判断集中一处，不复制多份登录态判断。复杂业务视图（导入预览/订单/用户/付款录入）本轮
只做"移到新路由 + 加模块头 + 去重复入口"，不重写其内部逻辑。

### 预计修改/新增文件
- 改：`frontend/src/App.vue`（路由壳、门户、模块页路由、付款中心、去重复导航、旧地址重定向）、`frontend/src/style.css`（全站居中 + 门户/卡片/金额框 + 响应式 5 档）、`frontend/src/api/client.ts`（如需付款方式可用性/费率辅助）。
- 新：`frontend/src/components/{ModuleCard,PortalStatusBar,PaymentMethodSelect,AmountSummary,QrDisplay}.vue`、若干前端测试、本日志。
- 后端：仅在"与付款方式直接相关"确有必要时改（预计无需——费率已在后端，用户端复用同公式展示即可；如需暴露费率常量再评估）。

### 是否需要数据库迁移
否。

### 阶段一结论
调查完成，路由/模块/组件方案如上。进入阶段二（角色门户与模块门户）。

---

## 阶段二～七：实现（完成，等待人工验收）

### 阶段二/三/四：三级门户与模块分流（手写路由扩展，未引入 vue-router）
- `RouteName` 扩展；`routeFromPath` 支持三级路径；`canonicalPath` 把旧地址重定向到新结构（`/admin/imports→/admin/data/import`、`/admin/imports/{id}→/admin/data/history/{id}`、`/admin/payments→/admin/finance/payments`、`/admin/payments/{id}→…/{id}`、`/admin/payment-qr→/admin/finance/qr-codes`）；`navigate/applyRoute/popstate/onMounted` 统一走 `handleRouteChange`。
- `isAdminRoute`=`routeName.startsWith('admin-')`（业务页）；`isUserRoute`=`startsWith('query-')`；`admin`/`query` 为门户/登录页。
- 深链保护：未登录进 `/admin/*`→记 `pendingAdminTarget`→跳 `/admin`；未登录进 `/query/*`→记 `pendingQueryTarget`→跳 `/query`；登录后回原目标或门户。角色不串入另一入口。
- **管理员**：`/admin` 门户仅 4 张模块卡（数据导入中心/订单管理/用户与账号/收付款管理）+ 顶部 `PortalStatusBar`（系统名/身份/后端状态/刷新/退出/返回入口）；**门户与 `/admin/data` 均不出现 Excel 文件框**，需再进 `/admin/data/import` 才见上传页。模块内 `module-subnav`（数据：Excel 导入/导入历史；收付款：付款记录/收款二维码）。删除了旧的「顶部一排 nav.tabs + 正文一排 action 按钮」重复入口。
- **普通用户**：`/query` 登录后为模块门户（4 卡：我的订单/付款中心/付款记录/账户安全 + 顶部 CN/退出/付款摘要）；业务拆到 `/query/orders`、`/query/payment`、`/query/payments`、`/query/security`。
- 组件化：新增 `components/ModuleCard.vue`、`components/PortalStatusBar.vue`（prop 驱动、可复用于两端门户与模块页）；业务态仍集中于 App.vue（路由壳/状态源），未无边界重写复杂业务视图。

### 阶段五：支付宝/微信选择与手续费联动（付款中心 `/query/payment`）
- **两个方式按钮恒显示**（`v-for="method in queryPayMethods=['alipay','wechat']`），支付宝蓝/微信绿等宽，默认选中支付宝；`queryMethodAvailable(method)` 只决定二维码显示与否，不影响可选。
- **金额三等尺寸框**：本金 / 手续费 / 本次应付（`query-amount-grid`），本次应付用 `metric-tile--emphasis` 放大红色。
- **手续费复用后端规则、以分为单位**：`queryFeeCents = wechat ? Math.floor((base+999)/1000) : 0`（0.1% 向上取整，无浮点四舍五入）；`queryPayableAmount=(base+fee)/100`。支付宝手续费恒 0.00；切换即时更新、不累计。浏览器实测：未付 120.00 → 微信手续费 0.12、应付 120.12；支付宝 0.00、应付 120.00。
- **二维码随方式切换**：显示对应方式二维码或中文空状态「管理员暂未配置…」，不显示另一方式的码、不用伪造图；点击放大灯箱；切换不刷新、不写付款记录。保留精简的「付款完成仍以管理员录入为准」说明。

### 阶段六：全站文字居中 + 响应式
- 新增居中层：标题/说明/按钮（flex 居中）/表单 label+input+placeholder/`th`+`td`/状态标签/金额/空错成态/筛选栏/面板头均居中；长文换行、控件等高、无裁切。
- 卡片/金额框等尺寸（`module-card` min-height、`query-amount-grid` 三等分）。
- 浏览器实测 320px：`/`、`/query`、`/query/payment`、`/query/orders`、`/admin`、`/admin/data`、`/admin/data/import`、`/admin/finance/payments`、`/admin/finance/qr-codes`、`/admin/users`、`/admin/orders` **均无页面级横向滚动**。

### 阶段七：测试
- `vue-tsc -b` 通过；`pnpm run build` 通过。
- 前端契约测试 **40 项全过**（更新 `login-entry`/`payment-qr-admin`/`payment-qr-user`/`query-user-view` 以匹配新结构；新增 `portal-routing.test.mjs` 锁定三级路由、旧址重定向、门户无文件框、组件抽取、无「音游窝」）。
- 后端本轮未修改（手续费规则已在后端，用户端复用同公式展示，无需改后端）。

### 修改/新增文件（本轮）
- 改：`frontend/src/App.vue`、`frontend/src/style.css`。
- 新：`frontend/src/components/ModuleCard.vue`、`frontend/src/components/PortalStatusBar.vue`、`frontend/tests/portal-routing.test.mjs`、本日志。
- 更新测试：`login-entry`/`payment-qr-admin`/`payment-qr-user`/`query-user-view`.test.mjs。

### 未使用子代理；未改真实业务数据；未迁移；未提交/推送。

---

## 阶段八：启动服务并进入人工验收停止点（2026-07-17 复核）

重新以隔离环境启动预览并复验（生产完全未触碰）：
- 临时后端 `:8090` ← 独立库 `pjsk_qr_dev`（迁移 0020 已应用）；先清掉一处历史遗留的旧 `pjsk-qr-dev` 进程再干净启动，健康检查 `{"status":"ok","database":"connected"}`。
- 前端 Vite `:5173`（`VITE_BACKEND_TARGET=http://127.0.0.1:8090`）。
- 种子数据（程序生成、非真实）：管理员 `qa_admin`；用户 `测试CN01`；订单总额 145、已付 25（色纸）、未付 120；1 条正常 + 1 条已撤销付款；支付宝配置无效测试二维码、微信未配置。

自动化复验：`vue-tsc -b` 通过；`pnpm run build` 通过；前端契约测试 **40/40 通过**。

浏览器复验（DOM 断言）：
- `/`：品牌「PJSK 谷子系统」+ 两个入口，无旧导航、无「音游窝」、无横滚。
- `/query` 登录后=用户门户：仅 4 张模块卡（我的订单/付款中心/付款记录/账户安全），**无业务表格、无二维码**，CN 显示。
- `/query/payment`：**支付宝 + 微信两个按钮均显示**（默认支付宝），支付宝→本金120.00/手续费0.00/应付120.00 且显示支付宝二维码；切微信→本金120.00/**手续费0.12/应付120.12**、标题「微信收款二维码」、微信未配置显示中文空状态（不显示支付宝码）。
- `/admin` 登录后=管理员门户：仅 4 张模块卡，**无 Excel 文件框、无旧 nav.tabs、无重复 action 按钮行**。
- `/admin/data`：仅 2 张卡（Excel 导入/导入历史）无文件框；`/admin/data/import`：出现文件框 + 模块内子导航。
- 旧地址重定向：`/admin/payment-qr`→`/admin/finance/qr-codes`、`/admin/imports`→`/admin/data/import`。
- 320px：`/`、`/query`、`/query/payment`、`/query/orders`、`/admin`、`/admin/data`、`/admin/data/import`、`/admin/finance/payments`、`/admin/finance/qr-codes`、`/admin/users`、`/admin/orders` **全部无页面级横向滚动**；Console 无红色错误。

**已停在人工验收点：未提交、未推送、未执行正式迁移、未上传真实二维码、未修改真实业务数据、未使用子代理。等待用户人工查看反馈。**

---

## 人工验收失败记录（2026-07-17 第二轮）与修复计划

用户人工验收未通过，三个问题需一起修复：
1. **普通用户登录页未与管理员登录页统一**：用户登录仍是横向 `login-form query-login`（CN/查询码/按钮挤一排），管理员登录已是纵向 `entry-card`/`entry-form`。要统一到同一套登录卡片基础样式（纵向：标题「用户登录」→CN 标签+输入→查询码标签+输入→查询按钮；辅助入口移到按钮下方独立区）。
2. **分流页中轴未对齐**：`.entry-choices` 用 `grid-template-columns: repeat(2, minmax(0,260px))` 但容器 `width: min(100%,620px)`——网格宽度(620)>两列内容(2×260+gap)，列在网格内左对齐导致整体视觉左偏，与标题中轴不齐。需让卡片组与标题共用同一居中容器、`justify-content:center`、两卡等宽。
3. **管理员筛选未完成**：订单/用户与账号/付款记录/导入历史需补齐完整筛选，紧凑工具栏 + 高级筛选折叠 + 生效筛选标签 + 清空全部 + 结果数量 + 可组合；技术字段（批次ID/SKU/SHA/主键）移入默认收起的高级/技术区；数据大时后端分页+查询参数。

现有后端筛选参数（只读确认）：
- 订单 `orders.List`：已支持 cn/project/project_id/item/series/category/role/import_batch_id/status/created_from/created_to；**缺** 付款状态、金额范围、数量范围。
- 付款记录 `payments.ListPaymentRecords`：已支持 cn/payment_method/status/paid_from/paid_to；**缺** 金额范围（本金/手续费/实付）、是否撤销（可由 status=voided 表达）。
- 用户 `users.List`：仅 cn/status；**缺** 查询权限/是否设查询码/是否绑邮箱/金额+订单数范围/最近登录。
- 导入历史 `importpreview.List`：无筛选参数；**缺** 文件名/Sheet/标题/状态/时间/行数/金额。

### 计划修改文件
- 前端：`App.vue`（用户登录卡片化、分流页容器、管理员各页筛选 UI + 高级折叠）、`style.css`（登录卡片统一、分流中轴、筛选工具栏/折叠样式）。
- 后端（视完成范围）：`orders/handler.go`（+payment_status/amount/quantity 范围）、`payments/handler.go`（+金额范围）、`users/handler.go`（+多维筛选）、`importpreview/handler.go`（+导入历史筛选）——均为新增查询参数，不改真实数据、不迁移。
- 后端改动需跑 `go fmt/build/vet/test`。

### 执行顺序
先修 #1 用户登录（快、可见）→ #2 分流中轴（快）→ #3 管理员筛选（大，分页优先订单/付款，逐页推进并追加日志）。

---

## 第二轮修复实现（2026-07-17）

### 问题 #1：普通用户登录页与管理员登录页统一（完成）
- 用户登录/绑定/找回三视图全部改用与管理员一致的 `entry-page > entry-brand > entry-card > entry-form` 结构（纵向）：标题「用户登录」→ CN 标签+输入 → 查询码标签+输入 → 查询按钮（宽度与输入一致）；辅助入口（忘记查询码/首次设置）移到按钮下方独立 `entry-aux` 区。登录/限流/绑定/邮箱找回/错误提示逻辑未改，仅布局与组件复用。
- 浏览器实测：`/query` 未登录 = `entry-card`、`entry-form` 纵向（flexDirection=column）、标题「用户登录」、CN/查询码两独立标签、辅助区存在。

### 问题 #2：分流页中轴对齐（完成）
- 根因：`.entry-choices` 用固定 `repeat(2, minmax(0,260px))` 但容器 `min(100%,620px)`，列在网格内左对齐 → 整体左偏。修复为 `repeat(2, minmax(0,1fr)) + justify-content:center + width:min(100%,560px) + margin:0 auto`。
- 浏览器实测：标题中心 X=160、卡片组中心 X=160、**axisDiff=0**、两卡等宽（302/302）。

### 问题 #3：管理员筛选（订单 + 付款记录已完成；用户与账号、导入历史列为下一子阶段）
- **后端新增查询参数**（不改数据、不迁移，`go fmt/build/vet/test` 全通过）：
  - `orders.List`：`payment_status`、`amount_min/max`（`o.total_amount`）、`quantity_min/max`（HAVING 汇总数量）。
  - `payments.ListPaymentRecords`：`principal_min/max`（本金）、`fee_min/max`（手续费）、`payable_min/max`（实付）。
  - 直连实测：`amount_min=200`→0 条、`amount_min=100`→1 条、`payment_status=paid`→1 条。
- **前端筛选重排**（订单/付款记录）：紧凑工具栏（订单：CN/谷子名称/订单状态/付款状态；付款：CN/付款方式/付款状态/是否撤销）+ `<details>` 高级筛选折叠（系列/种类/角色/金额范围/数量范围/时间；付款：时间/本金/手续费/实付范围）+ **导入批次 ID 等技术标识移入高级区内的技术分组**；`清空全部筛选（N）`（禁用态感知）+ `结果：N 条` 计数；所有筛选可组合、走后端参数。
- **下一子阶段（待续，本轮未完成）**：
  - 用户与账号：查询权限/是否设置查询码/是否绑定邮箱/金额+订单数范围/最近登录时间（需 `users.List` 增查询参数与 SQL 条件）。
  - 导入历史：文件名/Sheet/标题/状态/时间/行数/金额（需 `importpreview.List` 增筛选参数）。
  - 这两页当前仍为既有筛选；将按同一「紧凑工具栏 + 高级折叠 + 清空全部 + 结果计数」范式补齐并后端分页。

### 测试
- `vue-tsc -b` 通过；`pnpm run build` 通过；前端契约测试 40/40 通过；后端 `go build/vet/test` 全通过（orders/payments 含新参数）。

### 未提交、未推送、未迁移、未改真实数据、未用子代理。仍停在人工验收点。

---

## 第三轮人工验收失败记录（2026-07-17）与十二阶段计划

用户第三轮验收未通过。核心原则变更：**废弃「高级筛选」展开区**，管理员列表统一改为 **WPS 表头漏斗筛选**
（表头漏斗图标 → 浮层：搜索框 / 全选(N) / 空白(N) / 去重值+数量 / 多选 / 取消·确定 / 已筛选态 / 多列组合 /
**筛选作用于后端完整数据而非当前页**）。另含字体层级、名称统一、我的订单重排、技术标识隐藏、导出按钮分行、
导入中心重排、二维码中轴、组件抽取等共 12 阶段。

### 只读调查结论
- 名称散落：`用户服务台`/`返回服务台`/`谷子管理工作台`/`返回工作台`/`返回入口选择` 各处并存。
- 字体：模板内零散写字号，无统一层级变量；标签比数据值更小更淡，层级倒置。
- **后端无 facets 端点、无多值筛选参数、无分页总数**：WPS 表头筛选需要
  1) 新增「列去重值 + 每值数量 + 空白数 + 总数」的 facets 接口（对**全量数据**、并应用其他列已生效筛选）；
  2) 现有单值筛选参数改/扩为**多值**（如 `cn=a&cn=b` → SQL `IN`）；
  3) 列表接口返回**总数**以支持分页与「结果数量」。
  这三项是第四/六/七阶段的前置条件，工作量集中在 `orders`/`users`/`payments`/`importpreview` 四个 handler。

### 阶段一：统一字体层级（完成）
在 `style.css` 建立**唯一**字体层级来源（CSS 变量 + 通用类），不再逐页写字号：
- 变量：`--fs-page-title/section-title/value/label/hint/amount`、`--fw-*`、`--color-title/value/label/hint/amount(-danger)`、`--lh-*`。
- 通用类：`.t-page-title`（最大/居中/稳定间距）、`.t-section-title`（明显大于标签）、`.t-value`、`.t-label`、`.t-amount`（等宽数字 `tabular-nums`）、`.t-amount--danger`、`.t-hint`、`.t-hint--long`（长说明左对齐，避免为居中牺牲可读性）。
- **关键规则落地**：数据值 > 字段标签（更大 15px/600 vs 13px/500、颜色更深），修正此前「标签比内容更抢眼/更小得难看」的层级倒置。
- 既有结构接入统一层级：`portal-hero__title`、`module-header__title`、`panel__header h2/h3`、`metric-tile span/strong`（标签小弱、数值 20px/700 等宽数字）、`table th/td`（表头与内容各自统一、`vertical-align:middle` 消除同行垂直错位）、筛选与表单 label/input。

### 阶段二：门户与返回按钮名称统一（完成）
全局替换（App.vue 内旧名称出现次数已归零）：
- `用户服务台` → **用户中心**；`返回服务台` → **返回用户中心**
- `谷子管理工作台` → **谷子管理中心**；`返回工作台` → **返回谷子管理中心**
- `返回入口选择`（4 处） → **返回系统主页**
测试同步：`login-entry.test.mjs` 新增「命名统一」断言（旧名称零出现 + 新名称存在），并把原 `返回入口选择` 断言改为 `返回系统主页`。

### 阶段一/二验证
`vue-tsc -b` 通过；`pnpm run build` 通过；前端契约测试 **41/41 通过**。

### 尚未开始（第三～十二阶段，需继续）
3 我的订单重排；4 管理员订单 WPS 表头筛选；5 技术标识彻底隐藏；6 用户与账号 WPS；7 收付款 WPS；
8 导出按钮与标题分行；9 数据导入中心重排；10 二维码中轴；11 筛选组件抽取（ColumnFilterButton/
ColumnValueFilter/ColumnRangeFilter/ColumnDateFilter/ActiveFilterSummary）；12 专项测试与验收。
其中 4/6/7/11 依赖上述**后端 facets + 多值筛选 + 分页总数**三项前置改造。

未提交、未推送、未迁移、未改真实数据、未用子代理。

---

## 第四阶段接手只读调查（2026-07-17，Codex 续接）

### 基线与工作树
- 当前分支为 `main`；`HEAD` 与 `origin/main` 均为 `c8e7df0d7143137b8f6374d2b376a42bb8530582`，与交接基线一致。
- 工作树包含多轮未提交修改；本次调查未覆盖、删除或回退任何既有修改。
- `git diff --check` 未报告空白错误；仅出现用户级 Git ignore 文件无权限及 `style.css` CRLF/LF 提示。

### 第三阶段结论
- 第三轮计划中的“阶段 3：我的订单重排”**尚未完成**：本日志最后状态仍明确列为“尚未开始”，此后没有阶段 3 完成记录。
- 当前 `/query/orders` 已有此前模块路由拆分、分类/角色/系列/付款状态本地筛选、桌面表格和移动卡片；这些属于前序页面拆分与独立筛选实现，不能替代第三轮计划中的专门重排验收。
- 因当前任务要求先收口阶段 4，本轮不继续修改普通用户“我的订单”，将其列为下一阶段。

### 第四阶段真实停止点
- 已存在但尚未完成验证的后端文件：`backend/internal/orders/filters.go`、`facets.go`、`handler.go` 及对应测试，`backend/internal/api/router.go`、`order_facet_routes_test.go`，以及 `backend/internal/export/handler.go`。
- 已存在但尚未完成验证的前端文件：`frontend/src/filters/columnFilters.ts`，`ColumnFilterButton.vue`、`ColumnValueFilter.vue`、`ColumnRangeFilter.vue`、`ColumnDateFilter.vue`，`frontend/src/api/client.ts`、`App.vue`、`style.css`、`frontend/tests/order-column-filters.test.mjs`。
- 只读检查未发现半截 `<template>`、明显重复的管理员订单模板或肉眼可见的未闭合语法；新增组件导入均已在管理员订单模板使用。但 TypeScript、Vue 模板编译和未使用项仍需以后续 `vue-tsc`/构建确认，不能仅凭文本检查宣布通过。
- WPS 筛选 CSS **已经实际写入** `style.css`：包含漏斗活跃态、视口固定浮层、候选列表、加载/错误态、范围输入、分页、宽表内部滚动及 560px 以下底部面板规则，不是停在“准备追加”状态。
- 管理员订单页已移除旧顶部筛选表单和“高级筛选”块，现有模板已接入 8 个值筛选、4 个范围筛选、1 个日期筛选、后端分页、结果总数、清空全部筛选和三行标题/导出/结果结构；旧高级筛选仍存在于付款记录页，但不在管理员订单模板内。
- 列表后端已接入重复 query 参数多值筛选、参数化 `ANY`/数组 overlap、金额/数量/日期范围、分页、独立 count 和 `{items,page,page_size,total,total_pages}` 响应；非法分页与非法范围当前返回 400，分页大小有上限。
- facets 路由为精确路径 `/api/admin/orders/facets`，同时保留 `/api/admin/orders/` 详情前缀；路由测试已新增，静态 facets 路径按当前 `ServeMux` 最长匹配规则不会落入详情 handler。
- facets 已实现“应用其他列筛选、排除当前列自身值筛选”、去重候选、每值订单数、候选分页、搜索转义与空白候选。但当前 `total` 表示“候选值数量”，响应没有独立 `blank_count` 和“完整筛选结果订单总数”字段，尚未完全满足交接要求。
- 当前空白候选以重复参数的空字符串传输，`valueSet` 会把纯空白 trim 成空字符串并视为“筛选空白”；这与“空字符串和纯空白查询参数应忽略”的要求冲突，需改为无歧义的空白值编码后再补测试。
- 订单导出 handler 已调用 `orders.FiltersFromQuery` 与 `orders.BuildExportOrderIDsQuery`，并显式剔除 `page/page_size`，因此代码路径目标是导出全部匹配订单而非当前页；但尚缺 export handler 级测试证明 query 参数、ID 子查询和最终明细导出语义完整一致，暂不能认定收口。
- 管理员订单主表当前只显示业务字段与详情按钮；数据库 ID 仅用于详情跳转、不直接显示。订单详情底部存在默认收起的“技术标识”区并标注“仅供技术排查”。普通用户 API 类型与模板未发现这些技术字段。

### 与本阶段无关的历史未提交修改
- 明确存在：付款二维码后端/迁移/路由测试（`backend/internal/paymentqr/`、`0020_payment_qr_codes.sql`、相关 middleware/router/API/frontend 改动）、门户与模块卡组件、登录/路由/付款页面测试、付款与用户查询 handler、HANDOVER、7 月 16/17 日既有日志，以及本地导出的 3 个 `.xlsx` 文件和 `.claude/settings.local.json`。
- 上述文件均保持原状；阶段 4 后续只做必要的订单筛选收口与对应测试/日志，不擅自清理或纳入其他范围。

### 本调查阶段状态
- 测试：尚未运行（先完成只读停止点确认并记录日志）。
- 已知问题：facets 响应口径、空白参数编码、导出 handler 测试、TypeScript/构建/完整回归与浏览器验收待完成。
- 未提交、未推送、未执行迁移、未连接或修改真实业务数据、未使用子代理。

## 阶段 4B：订单导出筛选一致性（2026-07-17，完成）

### 修改文件
- `backend/internal/export/handler.go`
- `backend/internal/export/handler_test.go`

### 实现与语义
- 抽出 `parseOrderItemExportFilters`：只剔除列表分页 `page/page_size` 与导出专用 `unpaid_only`，其余重复值、空白标记、金额/数量范围和日期范围全部复用 `orders.FiltersFromQuery`。
- 抽出可测试的 `buildOrderItemExportQuery`：订单集合继续复用 `orders.BuildExportOrderIDsQuery`，因此与列表使用同一 `baseCTE + buildConditions`；订单明细导出只在该完整订单集合上展开。
- 移除订单明细导出的静默 `LIMIT 50000`。当前 CSV/XLSX 都导出全部匹配结果，不受当前页、每页条数或隐藏上限截断；`unpaid_only` 仍作为订单筛选之上的明细级附加条件。

### 测试
- `go fmt ./internal/export/...`：通过。
- `go test ./internal/export/...`：通过；数据库集成用例默认按安全门禁跳过，不读取 `DATABASE_URL`，本轮未启用数据库集成环境。
- 新增纯单元测试验证：重复 CN、多值+空白系列、金额和日期条件进入同一订单筛选 SQL；无 `LIMIT/OFFSET`；即使 URL 携带非法/超大列表分页参数，导出也会忽略分页而使用完整筛选。

### 状态
- 未提交、未推送、未执行迁移、未连接或修改真实业务数据、未使用子代理。

## 阶段 4A：后端筛选、分页与 facets 收口（2026-07-17，完成）

### 修改文件
- `backend/internal/orders/filters.go`
- `backend/internal/orders/facets.go`
- `backend/internal/orders/filters_sql_test.go`
- `backend/internal/orders/handler_test.go`
- `backend/internal/orders/facets_test.go`
- `frontend/src/filters/columnFilters.ts`、`frontend/src/api/client.ts`（同步空白参数与 facets 响应契约）

### 接口与参数语义
- 列表仍使用 `GET /api/admin/orders`，返回 `{items,page,page_size,total,total_pages}`；值筛选通过重复参数传递，多列组合后在后端完整数据上 count + 分页。
- facets 仍使用 `GET /api/admin/orders/facets`，参数为 `column/search/facet_page/facet_page_size` 加当前完整筛选；当前列自身值筛选在 SQL builder 中跳过，其他列值筛选以及金额/数量/日期范围继续生效。
- facets 响应补 `blank_count`；原 `total` 保持“搜索后去重候选值总数”口径，`values[].count` 为该值对应的去重订单数，空白候选仍以 `values[].blank=true` 和中文标签 `(空白)` 返回。
- 普通值参数中的空字符串和纯空白现在忽略；空白单元格筛选改用独立 `<column>_blank=1`，避免空输入与“筛空白”冲突，也避免保留字与真实业务值碰撞。
- 金额、数量、已付、未付范围新增 `min <= max` 校验；日期新增起止顺序校验。负数、非法格式、非法/超大分页继续明确返回 400；页码超出结果范围时仍返回空当前页及真实 `total/total_pages`，不会退化成无界查询。
- SQL 列表达式只从固定映射取值；用户值、搜索、范围、分页全部使用绑定参数。

### 针对性验证
- `go fmt ./internal/orders/...`：通过。
- `go test ./internal/orders/...`：通过。
- `go test ./internal/api -run 'TestOrderFacetsRoute'`：通过，确认精确 facets 路由受鉴权保护且不会落入 `/{id}` 详情 handler。
- 新增/更新覆盖：空值忽略、专用空白标记、金额/数量倒置范围、倒置日期范围、facets 独立空白数量 SQL。

### 状态
- 已知问题：导出 handler 级一致性测试、前端编译/契约测试与浏览器验收仍待后续阶段。
- 未提交、未推送、未执行迁移、未连接或修改真实业务数据、未使用子代理。

### 日志顺序说明（只追加更正）
- 本次追加 4B 时，补丁上下文命中了调查段末尾，导致文件中的显示顺序为“调查 → 4B → 4A”；实际执行顺序仍为“调查 → 4A → 4B”。依照历史开发日志禁止删除、移动或重命名的规则，不回写或搬动已落盘段落；以本说明及各段标题为准。

## 阶段 4C：前端表头筛选组件收口（2026-07-17，完成）

### 修改文件
- `frontend/src/components/ColumnFilterButton.vue`
- `frontend/src/components/ColumnValueFilter.vue`
- `frontend/src/components/ColumnRangeFilter.vue`
- `frontend/src/components/ColumnDateFilter.vue`
- `frontend/src/filters/columnFilters.ts`
- `frontend/src/api/client.ts`
- `frontend/src/style.css`

### 完成内容
- 继续复用既有组件边界，没有把筛选逻辑塞回 `App.vue`；浮层壳继续统一负责外部点击、Esc、焦点返回、视口夹紧和移动端底部面板。
- 值筛选保留搜索、防抖、全选/半选、逐值多选、每值数量、取消/确定、加载/空结果/失败重试及候选分页；失败信息统一增加中文前缀。
- 使用 facets 的 `blank_count` 在首批候选中固定提供 `(空白)` 项，避免去重值很多时空白项被分页藏到末页；后续分页会过滤服务端重复空白候选。
- 筛选状态编码改为重复普通参数 + `<column>_blank=1`；空白和普通值语义无歧义。
- `style.css` 已包含漏斗生效态、浮层、候选、范围/日期输入、移动端底部面板，并新增 `html/body/#app` 的 `max-width:100% + overflow-x:clip` 约束，320px 下宽表只在 `.table-scroll` 内滚动。

## 阶段 4D：管理员订单页面接入收口（2026-07-17，完成）

### 修改文件
- `frontend/src/App.vue`
- `frontend/tests/order-column-filters.test.mjs`

### 完成内容
- 管理员订单模板保持三行顶部结构，旧筛选表单/高级筛选未回归；8 个值列、4 个范围列、创建日期列均使用表头组件。
- 列表、facets、CSV/XLSX 导出继续共用 `orderFilterParams()`；筛选变化回第 1 页，翻页保留筛选，清空全部一次重置所有列。
- 新增每页 25/50/100/200 条选择器，修改每页条数后回第 1 页；页码、总页数和后端完整筛选总数清楚显示。
- 列表和 facets 请求失败增加中文错误前缀；401 仍回到登录状态，不导致页面崩溃。
- 修复 `clearAllFilters` 与旧付款筛选同名冲突，以及值筛选组件未使用模板 ref 的 TypeScript 错误。
- 主表只展示业务字段；数据库 ID 仅作为详情导航 key，不渲染。详情底部技术区仍默认折叠并显示“仅供技术排查”；普通用户模板/API 类型不含批次 ID、SKU、SHA、数据库主键或来源内部标识。

### 针对性验证
- 首次 `pnpm.cmd exec vue-tsc -b`：因 pnpm exec 在当前 Windows 环境未解析到实际存在的 `node_modules/.bin/vue-tsc.CMD` 而失败，非源码诊断。
- `pnpm.cmd run build`：修复 3 个 TypeScript 诊断后通过（该脚本实际执行 `vue-tsc -b && vite build`）。
- `node --test tests/order-column-filters.test.mjs`：27/27 通过；覆盖旧高级筛选消失、组件接入、多值/空白参数、筛选重置页码、总数与分页、facets 携带其他筛选、技术标识、移动端和 320px 溢出约束等。

### 状态
- 完整前后端回归和浏览器验收尚待阶段 4E。
- 未提交、未推送、未执行迁移、未连接或修改真实业务数据、未使用子代理。

## 阶段 4E：完整测试与隔离浏览器验收（2026-07-17，完成）

### 完整自动测试
- 后端（显式使用仓库缓存目录）：
  - `go fmt ./...`：通过。
  - `go test ./internal/orders/...`：通过。
  - `go test ./internal/export/...`：通过。
  - `go test ./...`：通过；数据库集成门禁未开启，未连接任何业务数据库做集成写入。
  - `go vet ./...`：通过。
  - `go build ./...`：通过。
- 前端：
  - `node_modules/.bin/vue-tsc.CMD -b`：通过。
  - `pnpm.cmd run build`：通过（Vue TypeScript + Vite production build）。
  - `pnpm.cmd test`：69/69 通过。
  - `node --test tests/*.test.mjs`：69/69 通过。
  - `git diff --check`：无补丁空白错误；仅保留 `style.css` CRLF/LF 工作树提示。

### 浏览器发现并修复的问题
- 首次金额范围实测时，浏览器把 `<input type="number">` 的 `v-model` 草稿变为 number，`ColumnRangeFilter.confirm()` 对其直接调用 `.trim()`，导致“确定”报 TypeError、浮层不关闭。
- 修复为 `String(value ?? '').trim()`，并新增专项契约测试；修复后重新运行完整前端回归，最终 69/69 通过。
- 修改文件：`frontend/src/components/ColumnRangeFilter.vue`、`frontend/tests/order-column-filters.test.mjs`。

### 隔离环境边界
- 只读确认 8090 原进程为 `%LOCALAPPDATA%\Temp\pjsk-qr-dev.exe`，本机数据库同时存在 `pjsk` 与独立 `pjsk_qr_dev`。
- 仅停止旧临时 PID 23376，并把当前代码构建到 `C:\tmp\pjsk-order-filters-dev.exe`；以显式 `DATABASE_URL=<LOCAL_ISOLATED_DATABASE_URL>`、`APP_PORT=8090`、`SERVER_HOST=127.0.0.1` 隐藏启动。正式 8080/`pjsk` 未访问、未停止、未修改。
- `/health` 返回 200 且数据库连接正常；沿用浏览器中的“验收测试管理员（测试数据）”会话和既有测试订单，只读查询，无新增/修改/删除订单、付款或用户。

### 浏览器验收结果
- 默认加载：1 条测试订单；后端分页响应正确显示“结果：1 条 / 第 1/1 页 / 每页 50 条”，金额 145.00、已付 25.00、未付 120.00、付款状态“部分付款”。
- 表头漏斗：8 个值列、4 个范围列、日期列均显示；生效后 `data-filtered=true`，清空后恢复 false。
- 值筛选：CN facets 返回测试 CN 与数量 1；搜索不存在值显示中文空状态；全选与取消全选状态切换成功；取消不应用；CN + 项目两列组合后结果仍为 1，两个漏斗均显示生效；清空全部恢复无筛选。
- 范围/日期：金额 100–150、数量 5–5、创建日期 2026-07-17 当天均匹配 1 条；单列清除成功。倒置金额 200–100 显示中文错误“订单列表加载失败：amount_min 不能大于 amount_max”，清空后恢复。
- 分页：每页条数由 50 切换为 25 后仍显示第 1/1 页；现有隔离数据只有 1 单，无法动态触发上一页/下一页，多页行为由后端分页与前端专项测试覆盖。
- 导出：在金额最小 200、结果 0 条的筛选状态点击 CSV；浏览器下载事件未被控制层捕获，但隔离后端日志明确记录 `GET /api/admin/export/order-items.csv` 成功返回且无 error。完整筛选/无分页 SQL 由 export 单元测试覆盖。
- 技术标识：主表无批次 ID/SKU/SHA/数据库主键/来源内部标识；订单详情仅有一个技术 `<details>`，`open=false` 且包含“仅供技术排查”。普通用户模板/API DTO 的无技术字段契约测试通过。
- 320px：`documentScrollWidth=documentClientWidth=305`，无页面级横滚；订单表自身 `clientWidth=257 / scrollWidth=1842`，只在表格容器内部滚动。筛选浮层为 `is-mobile` 底部面板，左右 8px、底部贴合视口；Esc 与点击外部均可关闭。
- Console：修复后验收时间段无新的页面 error；早先两条 `.trim()` error 属于已修复前的实测记录。

### 受现有测试数据限制的动态项
- 隔离订单的业务值均非空，未为了验收修改测试数据，因此 `(空白)` 的真实选择无法动态触发；`blank_count`、首批空白候选、`<column>_blank=1` 和后端空白 SQL 已由专项测试覆盖。
- 隔离库只有 1 个订单，无法动态点击上一页/下一页；越界/分页总数/筛选重置页码由单元及契约测试覆盖。

### 最终状态
- 阶段 4 自动测试与可在既有隔离数据上完成的浏览器验收均通过；上述两个数据受限项有自动测试证据，无已知阻塞性代码问题。
- 临时 8090 当前运行的是本工作树构建、且只连接 `pjsk_qr_dev` 的验收后端；5173 仍为既有 Vite 预览，供用户人工验收。
- 未提交、未推送、未执行正式迁移、未修改真实业务数据、未使用子代理。

## 阶段 3A：普通用户“我的订单”只读调查（2026-07-17，完成）

### 当前页面与数据契约
- `/query/orders` 已由用户模块公共头部承载“我的订单”标题，并保留“← 返回用户中心”按钮；本阶段无需改动门户路由或返回逻辑。
- 当前页面使用本地 `queryOrderFilters` 按种类、角色、系列、付款状态筛选普通用户接口已返回的订单明细；`filteredQueryOrders` 会复制订单并过滤明细，不修改原始响应，订单筛选与付款记录筛选状态彼此独立。
- 当前桌面表格字段为“谷子名称、谷子系列、分类、角色、数量、单价、小计、已付、剩余未付、付款状态”；移动卡片沿用相近字段，存在顺序不合理、额外展示单价、业务标签不统一的问题。
- 四项订单汇总目前按每个订单重复挤在一行小标签中，页面顶部没有“总金额、共多少件、已付金额、未付金额”四个独立等尺寸汇总框；筛选结果也只在筛选生效时显示订单数，没有稳定显示匹配谷子件数。
- 普通用户接口 `GET /api/query/orders` 的响应继续仅包含用户业务身份、订单/付款业务 DTO 与总数量/总额/已付/未付汇总。订单 DTO 不序列化数据库主键、订单号、项目名/项目 ID、导入批次、SKU、SHA、文件名、来源 Sheet、来源位置或管理员信息；内部订单 ID 只在后端组装响应时使用。
- `goods_name` 与 `display_name` 仍同时下发，但当前订单页面只渲染一次 `display_name || goods_name`；本阶段不改接口，避免影响其他普通用户视图。

### 拥挤、重复与技术字段核查
- 拥挤点：每个订单重复四项汇总、桌面十列宽表、名称列缺少明确换行约束、移动卡片九行且包含不在本阶段优先字段中的单价。
- 重复点：订单级四项金额/数量与接口全局汇总表达重复；页面应改为顶部全局四框，并在订单分组中只保留明细数量提示。
- 普通用户模板未发现真实订单号、项目名、项目 ID、SKU、批次 ID、SHA、导入来源文件、来源 Sheet、管理员信息或数据库 ID 的渲染；“订单 1”只是前端分组序号，不是业务订单号。
- 当前根目录 `order-items-20260717-173125.csv` 是阶段 4 管理员订单明细导出，表头含项目、订单号、来源文件/Sheet/位置等管理员字段；它与普通用户 `/query/orders` 页面和接口无关，本阶段只读确认、不修改该文件。
- 阶段 4 新增的 `ColumnValueFilter`、`ColumnRangeFilter`、`ColumnDateFilter`、`ColumnFilterButton` 与管理员订单样式只服务管理员页面；普通用户页不接入、不复制、不改动这些 WPS 表头筛选组件。

### 本阶段预计修改
- `frontend/src/App.vue`：增加稳定的筛选结果件数计算，重排普通用户订单汇总、筛选、桌面表格和移动卡片；不改管理员订单模板。
- `frontend/src/style.css`：完善普通用户订单专属网格、表格内部滚动、名称换行、等宽数字和 320px 响应式约束；复用现有字体层级与颜色变量。
- `frontend/tests/query-user-view.test.mjs` 及普通用户订单专项测试：覆盖四框、业务筛选、字段顺序、两位金额、桌面/移动布局、技术字段隐藏和不使用管理员 WPS 组件。
- `backend/internal/query/` 当前不需要代码修改；完成前只运行现有查询与完整后端回归测试验证契约未受影响。
- 本调查仅执行只读 Git/源码/测试/CSV 检查；未提交、未推送、未迁移、未连接或修改真实业务数据、未使用子代理。

## 阶段 3B：普通用户订单视图与响应式重排（2026-07-17，完成）

### 修改文件
- `frontend/src/App.vue`
- `frontend/src/style.css`

### 页面重排
- 在“我的订单”模块头之后新增四个独立、等尺寸汇总框，顺序为“总金额、共多少件、已付金额、未付金额”；金额全部继续调用 `formatMoney` 显示两位小数，数量直接采用后端 `total_quantity`，已付/未付只使用低饱和边框、底色和文字色作区分。
- 汇总口径直接读取 `GET /api/query/orders` 的全局 `total_amount/total_quantity/paid_amount/remaining_amount`，不根据筛选结果重新推测金额或付款状态。
- 业务筛选整理为四列网格，仅保留“谷子种类、角色、系列、付款状态”；保留独立的“清空筛选”，新增始终可见的“筛选结果：N 项谷子明细（M 个订单）”。筛选仍由既有 `filteredQueryOrders` 在当前 CN 已加载数据上计算，并新增只读汇总计算 `filteredQueryOrderItemCount`。
- 删除每个订单头部重复拥挤的四项金额/数量小标签，只保留无业务编号含义的前端分组序号和当前分组明细项数。
- 桌面表格重排为“谷子名称、角色、谷子种类、系列、数量、总金额、已付金额、未付金额、付款状态”，移除单价列；名称允许自然换行，数量和金额使用 `tabular-nums`，宽表只在 `.table-scroll` 内部横向滚动。
- 移动端继续使用每项谷子一张卡片：名称与中文付款状态置顶，字段区依次为角色、谷子种类、系列、数量、总金额、已付金额、未付金额；移除单价和重复付款状态行。
- 720px 以下四框与筛选改为两列，400px 以下改为单列；520px 以下隐藏桌面表格、显示移动卡片，操作按钮纵向排布，延续全局 `html/body/#app overflow-x: clip` 的页面级防横滚约束。
- 复用阶段 1 的 `--fs-*`、`--fw-*` 字体层级和现有中文字体回退/数字字体规则；未复制或调用管理员 WPS 表头筛选组件。

### 状态
- 本批次只改普通用户订单模板、计算属性和专属样式；未修改普通用户接口、管理员订单页面、订单/付款计算、数据库结构或导出文件。
- 已执行局部 `git diff --check`：无空白错误；`style.css` 仍只有工作树既有的 CRLF/LF 提示。
- 自动测试与浏览器验收待后续阶段；未提交、未推送、未迁移、未连接或修改真实业务数据、未使用子代理。

## 阶段 3C：普通用户订单专项测试（2026-07-17，完成）

### 修改文件
- `frontend/tests/query-orders-layout.test.mjs`（新增）
- `frontend/tests/independent-filters.test.mjs`

### 覆盖内容
- 四个汇总框独立存在，且旧 `query-order-summary` 拼接式订单级汇总不再出现。
- 筛选区恰好包含谷子种类、角色、系列、付款状态，提供筛选结果项数与清空筛选，不含订单号、项目、SKU、SHA、批次或来源筛选。
- 桌面表格字段顺序与本阶段要求一致，移除单价/小计表头，三类明细金额与三类全局金额均通过 `formatMoney` 展示。
- 普通用户订单模板不渲染订单号、项目 ID/名称、批次、SKU、SHA、文件/Sheet/来源位置，也不接入四类管理员 WPS 表头筛选组件。
- 同时存在桌面表格和移动卡片；未付款、部分付款、已付款使用既有中文状态映射。
- CSS 契约覆盖页面级防横滚、宽表内部最小宽度、520px 桌面/移动视图切换、400px 单列汇总/筛选以及长名称正常换行。
- 更新独立筛选测试中的汇总口径提示契约，继续验证订单与付款记录筛选状态分离、筛选不修改原始订单响应。

### 针对性测试
- `node --test tests/query-orders-layout.test.mjs tests/query-user-view.test.mjs tests/independent-filters.test.mjs`：16/16 通过。
- 未删除或弱化既有测试；完整前后端回归与浏览器验收待后续阶段。
- 未提交、未推送、未迁移、未连接或修改真实业务数据、未使用子代理。

## 阶段 3D：普通用户订单自动测试与完整回归（2026-07-17，完成）

### 前端
- `node_modules/.bin/vue-tsc.CMD -b`：通过，无 TypeScript 诊断。
- `pnpm.cmd run build`：通过；脚本实际执行 `vue-tsc -b && vite build`，生产构建成功。
- `pnpm.cmd test`：75/75 通过。
- `node --test tests/*.test.mjs`：75/75 通过。

### 后端
- 测试进程显式移除 `DATABASE_URL` 与 `PJSK_TEST_DATABASE_URL`，只使用仓库内 Go 缓存目录，未启用数据库集成门禁。
- `go test ./internal/query/...`：通过。
- `go test ./...`：通过；无测试文件的命令包按 Go 标准输出跳过，其余包通过。
- `go vet ./...`：通过。
- `go build ./...`：通过。
- 本阶段没有修改 `backend/internal/query/`；回归用于确认普通用户业务 DTO、金额/付款后端口径和现有查询行为未受页面重排影响。

### Git 与状态
- `git diff --check`：无空白错误；仅保留 `frontend/src/style.css` 的既有 CRLF/LF 工作树提示。
- 当前工作树仍包含阶段 1、2、4、付款二维码等历史未提交成果及本阶段文件；未清理、回退、覆盖或纳入无关修改。
- 浏览器验收待阶段 3E；未提交、未推送、未执行正式迁移、未连接或修改真实业务数据、未使用子代理。

## 阶段 3E：普通用户订单隔离浏览器验收（2026-07-17，完成）

### 隔离环境确认
- 8090 当前监听进程 PID 28524，进程路径为 `C:\tmp\pjsk-order-filters-dev.exe`，启动时间仍为 2026-07-17 17:25:20；与阶段 4E 中以显式 `DATABASE_URL=<LOCAL_ISOLATED_DATABASE_URL>` 启动并记录的同一进程一致，中途未重启或替换。
- `GET http://127.0.0.1:8090/health` 返回 200、`database: connected`；5173 由现有 Vite 进程监听。本轮只使用隔离账号“测试CN01”读取 `/api/query/orders`，后端访问日志仅新增 config、health、query login/orders、付款二维码可用性与恢复邮箱状态的读取请求。
- 未访问正式 8080/`pjsk`，未新增、修改或删除订单、付款、用户或其他测试/真实数据。

### 桌面端验收
- `/query/orders` 显示“我的订单”和“← 返回用户中心”；四个汇总框为总金额 145.00、共 5 件、已付 25.00、未付 120.00，与隔离后端响应一致。
- 默认筛选结果为 4 项谷子明细、1 个订单；桌面表头顺序为“谷子名称、角色、谷子种类、系列、数量、总金额、已付金额、未付金额、付款状态”，付款状态均为中文，金额均两位小数。
- 普通用户页面文字与表头未发现订单号、项目名、SKU、SHA、批次 ID 等技术标识；桌面视口 1280px 时 `documentWidth=clientWidth=1265`，桌面表显示、移动卡片隐藏。
- 选择谷子种类“吧唧”后结果由 4 项变为 1 项，全局四项汇总保持 145.00/5/25.00/120.00；继续组合角色“初音未来”仍精确匹配同一明细。点击“清空筛选”后四个下拉均恢复空值、结果恢复 4 项、按钮重新禁用。

### 320px 移动端验收
- 临时视口设为 320×720：`documentWidth=bodyWidth=clientWidth=305`，没有页面级横向滚动；扫描未发现超出视口的元素，按钮之间无重叠。
- 四项汇总与四个筛选均为单列；桌面表 `display:none`，移动卡片容器 `display:grid`，显示 4 张卡片。
- 每张卡片均以谷子名称和中文付款状态开头，并依次展示角色、谷子种类、系列、数量、总金额、已付金额、未付金额；卡片最大右边界约 281.6px，小于 305px 客户区，无裁切证据。
- 验收后已重置临时视口，并保留已登录的 `/query/orders` 页面供人工验收。

### Console 与已知限制
- 本轮页面 Console error 日志为空，无新红色错误。
- 公共用户状态条仍按既有默认值显示“后端离线”，虽然 `/health` 和订单接口均正常；该状态文案属于阶段 2 公共门户状态传参范围，不影响订单页面数据与本阶段验收，本阶段遵守“只实施第 3 阶段”边界未扩展修改，列为后续独立核查项。
- 阶段 3 的代码、自动测试与可在既有隔离数据上执行的浏览器验收均完成；未提交、未推送、未执行正式迁移、未修改真实业务数据、未使用子代理。

## 阶段 4F-1：订单聚合问题调查（2026-07-17，完成）

人工验收发现 `/admin/orders` 的数据粒度设计错误：一行代表一个订单，同一订单的多个谷子名称、系列、种类、角色被拼接进同一格（截图中的 `MK-01、MK-02…`、`吧唧、立牌、色纸…`）。筛选虽能选出订单，却仍把订单内其他不匹配的谷子一起显示，无法看出究竟哪一项命中。本阶段先只读调查，不改代码。

### 基线
- 分支 `main`，HEAD `c8e7df0`，与 `origin/main` 一致；阶段 1/2/3/4、门户路由、付款二维码与迁移 `0020` 等成果均未提交，工作区完整保留。
- `git diff --check` 无空白错误（仅 `style.css` 的 CRLF 提示，属既有换行设置）。

### 调查结论（逐条对应问题清单）
1. **当前一行代表订单，不是订单明细。** 根因在 `backend/internal/orders/filters.go` 的 `baseCTE`：`order_agg` 子查询以 `group by o.id` 聚合，因此每个订单只剩一行。
2. **通过数组聚合的字段有四个**，均在 `order_agg` 中：`array_agg(distinct pr.name) as item_names`、`array_agg(distinct coalesce(pr.series_code,'')) as series_codes`、`array_agg(distinct coalesce(pr.category,'')) as categories`、`array_agg(distinct coalesce(pr.character_name,'')) as character_names`。前端 `joinColumnValues()` 再用 `、` 把数组拼成一格文本——即截图里的拼接来源。
   - 对应筛选条件用数组重叠 `b.series_codes && $n::text[]`（`buildConditions`），语义是"订单内只要有一项命中就整单返回"，这正是"筛选角色后仍显示其他角色"的直接原因。
3. **facets 数量口径是去重订单数**，不是明细行数：`facets.go` 的候选查询为 `count(distinct b.id)`，数组列先 `cross join lateral unnest(...)` 再按订单去重。
4. **金额、已付、未付当前是订单级**：`order_agg` 里 `sum(least(coalesce(ip.paid,0), oi.amount))` 与 `sum(greatest(oi.amount - coalesce(ip.paid,0),0))`；`total_amount` 直接取 `o.total_amount`。付款状态由订单级已付/未付推导。
5. **现有导出已经是"一项明细一行"，但筛选口径同样错误**：`export/handler.go` 的 `buildOrderItemExportQuery` 用 `o.id in (<BuildExportOrderIDsQuery>)`，即"匹配订单的全部明细"。筛选角色后导出仍会包含该订单其他角色的明细，与新需求不符，必须一并改为明细级。
6. **订单详情不依赖列表 DTO**：详情走 `OrderSummary`/`OrderDetail`（含 `import_batch_ids` 等技术标识），与列表的 `OrderListItem` 是两套结构；把列表改成明细行不影响详情页。
7. **需更新而非删除的阶段 4 测试**：`orders/handler_test.go`（多值筛选断言 `OrderListItem`）、`orders/filters_sql_test.go`（断言 `&&` 重叠与 `base` 形状）、`orders/facets_test.go`（断言 `count(distinct b.id)` 与 unnest）、`export/handler_test.go`、`frontend/tests/order-column-filters.test.mjs`（断言 `joinColumnValues` 聚合渲染）。WPS 组件、浮层交互、分页与导出跟随筛选等既有断言全部保留。

### 隔离性确认
- `internal/query`（普通用户"我的订单"）不 import `internal/orders`，仅 `internal/api/router.go` 与 `internal/export/handler.go` 依赖 orders 包；因此阶段 4F 的管理员 DTO 变更不会波及阶段 3 的用户接口。

### 修正方向（不在本阶段实施）
把 `baseCTE` 的一行从"订单"改为"订单明细"：去掉四个 `array_agg`，改为 `oi`/`pr` 的单值列；筛选由数组重叠改为明细行等值匹配；facets 计数改为明细行数；分页 `total` 改为明细行总数；导出改为按明细行条件筛选，不再按订单放大。

## 阶段 4F-2：明细列表后端（2026-07-17，完成）

把 `baseCTE` 的"一行"从订单改为订单明细，这是本阶段全部行为变化的唯一根源。

- 删除 `order_agg` 子查询及其四个 `array_agg`（`item_names`/`series_codes`/`categories`/`character_names`）与 `group by o.id`。`base` 改为直接由 `order_items oi` 驱动，join `orders`/`users`/`projects`/`products`，`where oi.revoked_at is null`。
- 每行的谷子名称、系列、种类、角色均为单值列（`item_name`/`series_code`/`category`/`character_name`，后三者 `coalesce(...,'')` 以保留空白候选语义）。
- **数量不展开**：明细数量为 3 仍是一行，`quantity` 列显示 3；SQL 中不存在 `generate_series`。
- 列表 DTO `OrderListItem` 改为明细行：`item_id`、`order_id`（仅作详情导航与前端 key，不作可见列）、`order_no`、`cn_code`、`display_name`、`project_name`、`item_name`、`series_code`、`category`、`character_name`、`quantity`、`unit_price`、`total_amount`、`paid_amount`、`unpaid_amount`、`status`、`payment_status`、`created_at`。不再返回任何拼接/数组字段。
- 排序 `order by b.created_at desc, b.order_id desc, b.sort_order, b.item_name, b.item_id`：同一 CN/订单的明细相邻且顺序稳定，`item_id` 兜底保证翻页不重不漏。
- 分页口径改为明细行：`total` 为匹配明细行总数，`total_pages` 由其计算。同一订单的明细可跨页，这是明细分页的自然结果。

### 金额口径（沿用数据库既有真实结果，未做任何估算）
- 复用既有 `payment_items` → `payments` 分配模型：`item_paid` 按 `order_item_id` 汇总 `applied_amount`，且 `filter (where pay.status = 'approved')`，因此**已撤销/未审批付款不计入**。
- 每行：`total_amount = oi.amount`；`paid_amount = least(coalesce(ip.paid,0), oi.amount)`（钳制到本行小计）；`unpaid_amount = greatest(oi.amount - coalesce(ip.paid,0), 0)`。满足 `未付 = 总额 - 有效已付`。
- 付款状态按本行自身已付/未付推导，不再看订单级汇总；因此部分付款的订单不会把它已付清的明细混进"未付"结果。
- 明细金额来自 `oi.amount`，行内不再出现 `o.total_amount`；同一订单各明细加总即订单口径。未发现无法可靠取得明细已付分配的阻塞点。

## 阶段 4F-3：明细级 facets 与筛选（2026-07-17，完成）

- `filterColumns` 全部改为单值列表达式，`buildConditions` 由数组重叠 `&&` 改为 `= any($n::text[])` 等值匹配。**这是"筛选角色后仍显示其他角色"的直接修复**：每行只能凭自身的值命中，不再因为"同订单某个兄弟明细匹配"而被返回。
- 范围筛选绑定本行自身数字：`b.quantity`、`b.total_amount`、`b.paid_amount`、`b.unpaid_amount`。
- facets 计数由 `count(distinct b.id)`（去重订单数）改为 `count(*)`（明细行数），并去掉 `cross join lateral unnest(...)`——base 已无数组可展开。`初音未来 38` 即"38 项谷子明细"，勾选后列表恰好返回 38 行。
- 作用域规则保持不变：取某列候选时应用其他列筛选、排除当前列自身筛选、保留范围与日期筛选；空白候选继续用 `<column>_blank=1`；空/纯空白参数忽略；全部参数化；搜索 `%`/`_`/`\` 仍转义。

## 阶段 4F-5：导出一致性（2026-07-17，完成）

- 发现导出虽已是"一项明细一行"，但筛选口径同样错误：`o.id in (<订单 id>)` 会把匹配订单的**全部**明细放大回来。
- `BuildExportOrderIDsQuery` 改为 `BuildExportItemIDsQuery`（`select b.item_id from base b where ...`），导出条件改为 `oi.id in (...)`。导出与列表共用同一 `base` 与同一份条件，因此行级一致：筛角色只导出该角色明细。
- 导出仍无 `LIMIT/OFFSET`，不跟随列表分页，也未恢复 50,000 行静默截断。
- **口径差异的处理**：原导出无条件排除 `o.status <> 'cancelled'`，与列表口径不一致。现将该排除**仅保留在 `unpaid_only`（付款页"未付明细"导出）路径**——不应催缴已取消订单；订单页自身的导出不再排除，从而与页面结果数量行级一致。

## 阶段 4F-4：前端表格接入（2026-07-17，完成）

- `OrderListItem`（`client.ts`）改为单值明细行：`item_id`/`order_id`/`item_name`/`series_code`/`category`/`character_name`/`quantity`/`unit_price`/… 不再有任何 `string[]` 字段。
- 主表列序：CN、用户名称、项目名称、谷子名称、谷子系列、谷子种类、谷子角色、数量、单价、明细总金额、已付金额、未付金额、订单状态、付款状态、创建时间、查看详情（16 列）。
- 删除 `joinColumnValues()` 及其全部调用——聚合渲染的入口被彻底移除，前端也不对聚合字符串做拆分。
- 表头筛选：CN/项目名称/谷子名称/系列/种类/角色/订单状态/付款状态用 `ColumnValueFilter`；数量/明细总金额/已付/未付用 `ColumnRangeFilter`；创建时间用 `ColumnDateFilter`；用户名称、单价、操作列不带漏斗。
- **不使用 `rowspan`**：同一 CN 连续多行是正确行为，合并会破坏筛选后的行展示、分页与移动端。改为 `isAlternateOrderRow(index)` 按订单交替浅底色（`.order-row--alt`）做视觉分组，内容不合并。
- 长名称改为换行（`.cell-wrap`，`white-space: normal` + `overflow-wrap: anywhere`），不再用省略号掩盖字段；表格 `min-width` 1180→1460px，仍只在 `.table-scroll` 内部横向滚动。
- 每行"详情"进入所属订单详情页（`/admin/orders/${item.order_id}`），详情页仍展示该订单完整内容。
- 说明文字改为"每行对应一项谷子明细，同一 CN 或同一订单可出现多行。筛选只保留符合条件的明细，不会把其他谷子合并显示。"（主说明）＋"本页只读，不允许修改、删除或撤销。"（浅色辅助说明）。结果文案改为"结果：共 N 项谷子明细"。

## 阶段 4F-6：测试与自动化验收（2026-07-17，完成）

既有阶段 3/4 测试全部保留，只更新因粒度变化而失效的断言，未删除或弱化任何一条。

### 后端新增/更新
- 新增 `TestBaseRowIsOneOrderItemNotOneOrder`：禁止 `array_agg`/`string_agg`/`item_names`/`series_codes`/`categories`/`character_names`/`group by o.id`/`order_agg` 回潮，并要求 base 由 `order_items` 驱动、排除 `revoked_at`。
- 新增 `TestQuantityIsNotExpandedIntoRows`：无 `generate_series`，数量 3 仍是一行。
- 新增 `TestProductFiltersMatchTheRowsOwnValue`：禁止 `&&` 重叠，要求四个产品列均为 `= any(...)` 等值。
- 新增 `TestMoneyAndPaymentStatusAreDetailLevel`：明细金额取 `oi.amount`、`paid` 钳制且仅 `approved`、行内不出现 `o.total_amount`、范围绑定本行数字。
- 新增 `TestPaymentStatusFilterUsesTheRowsOwnStatus`、`TestCountAndExportSharePreciselyTheListsConditions`（count/export 与列表条件逐字一致）。
- 新增 `TestFacetCountsDetailRowsNotOrders`、`TestFacetDoesNotUnnestAggregatedColumns`；更新 facet 计数与"排除自身列/保留其他列"断言。
- 更新 `export/handler_test.go`：要求 `oi.id in (`，并断言不得出现 `o.id in (`。
- 结果：`go fmt`/`go vet`/`go build` 无输出；`go test ./...` 全部包 ok。

### 前端新增/更新
- 新增：一行=一项明细且字段单值、无任何聚合渲染（含 `joinColumnValues`/`.join(`/`.split(` 均不得出现）、表头含数量/单价/明细总金额/已付/未付、结果文案为"谷子明细"、不使用 `rowspan=` 且有视觉分组、角色等列仍用 WPS 组件、详情入口指向 `item.order_id`、技术字段不在主表、DTO 无 `string[]`、阶段 3 契约不变。
- 更新：表头标签（商品总数→数量、订单总金额→明细总金额）、金额/状态改绑 `item.*`、长名称由省略号改为换行断言、`min-width` 1460px、结果文案。
- 结果：`vue-tsc -b` 通过；`vite build` 通过；`node --test tests/**/*.test.mjs` **pass 85 / fail 0**（其中订单筛选专项 38 项）。

## 阶段 4F-7：隔离环境验收（2026-07-17）

### 隔离边界（本轮重新确认，比上轮更严格）
- 停止旧临时 PID 28524，用当前工作树重新构建 `C:\tmp\pjsk-order-filters-dev.exe`（必须重启，否则 8090 仍跑改造前的聚合代码），以显式 `DATABASE_URL=<LOCAL_ISOLATED_DATABASE_URL>`、`APP_PORT=8090`、`SERVER_HOST=127.0.0.1` 启动，新 PID 27636，`/health` 返回 `{"status":"ok","database":"connected"}`。
- **数据库归属实证**（不再只依赖启动参数）：PID 27636 持有的 postgres 连接本地端口为 59168；从 `pg_stat_activity` 反查该连接 `datname = pjsk_qr_dev`。全程无任何指向正式 `pjsk` 的连接。正式 8080（PID 21248）未访问、未停止、未修改。
- **发现并修正了一个真实的隔离隐患**：`frontend/.env.development` 里 `VITE_API_BASE_URL=http://localhost:8080`，`vite.config.ts` 的代理默认值也是 8080；原 5173 进程（PID 4612）的启动命令行看不到任何覆盖，无法证明它连的是隔离后端。因此停掉该进程，用显式 `VITE_BACKEND_TARGET=http://127.0.0.1:8090` 重启 5173（新 PID 28336）。经查 `client.ts` 为 `import.meta.env.DEV ? '' : configuredApiBaseUrl`，开发模式一律走相对路径→Vite 代理，故代理目标才是唯一决定因素。隔离后端访问日志确认收到了 `/api/config`、`/api/admin/me`、`/api/admin/orders`，链路为 浏览器→5173→8090→`pjsk_qr_dev`。

### 用真实隔离数据验证明细粒度（读写边界：仅 SELECT）
以临时只读测试直接调用**真实的查询构造器**（`buildListQuery`/`OrderFacets`/`BuildExportItemIDsQuery`）连 `pjsk_qr_dev` 执行，验证后已删除该临时文件（未留在仓库）。结果：

- **核心问题已消除**：截图里那个显示为 `MK-01、MK-02…`／`吧唧、立牌、色纸…` 的订单（`387f3d5c`），现在返回 **4 行明细**，每行只有一个谷子名称/系列/种类/角色：
  - 初音未来吧唧 | MK-01 | 吧唧 | 初音未来 | 数量 2 | 单价 30.00 | 小计 60.00 | 未付 60.00
  - 宁宁色纸 | MK-02 | 色纸 | 宫本宁宁 | 数量 1 | 单价 25.00 | 小计 25.00 | 已付 25.00 | paid
  - 瑞希亚克力 | MK-04 | 亚克力 | 晓山瑞希 | 数量 1 | 单价 20.00 | 小计 20.00 | 未付 20.00
  - 奏立牌 | MK-03 | 立牌 | 朝比奈まふゆ | 数量 1 | 单价 40.00 | 小计 40.00 | 未付 40.00
- 同一 CN 连续出现 4 行；断言四个产品列均不含 `、` 拼接，全部通过。
- **数量不拆行**：初音未来吧唧数量为 2，仍是一行、数量列显示 2。
- **角色筛选只留对应明细**：`role=初音未来` → `total=1`，只返回初音未来那一行；宁宁/瑞希/まふゆ 不再混在结果里。
- **组合筛选**：`role=初音未来 + category=吧唧` → `total=1`，无不匹配行。
- **facets 为明细行计数且互相独立**：一个订单的四个角色返回四个独立候选 `朝比奈まふゆ=1, 初音未来=1, 宫本宁宁=1, 晓山瑞希=1`，计数合计 4 = 明细总数 4；种类、系列同理；`cn` 候选 `测试CN01=4`（该 CN 有 4 项明细）。
- **金额口径自洽**：明细加总 小计 145.00 / 已付 25.00 / 未付 120.00，与订单级 `total_amount` 合计 145.00 一致，也与阶段 4E 记录的 145/25/120 一致。
- **撤销付款不计入**：隔离库存在 1 条非 approved 付款，已付合计仍为 25.00，未被计入。
- **导出与列表行级一致**：未筛选时导出行数 4 = 列表 total 4；`role=初音未来` 时导出行数 1 = 列表 total 1；导出 SQL 无 LIMIT/OFFSET。

### 未能完成的部分（阻塞，如实记录）
- **管理员界面的可视验收未完成**。阶段 4E 是「沿用浏览器中已登录的『验收测试管理员（测试数据）』会话」，该会话在用户自己的 Chrome 里；本轮 Claude in Chrome 扩展未连接（两次重试均报 not connected），受控预览浏览器是全新上下文，`GET /api/admin/orders` 返回 401。
- 未采取绕过手段：不新建管理员账号、不代为输入密码（此前 2026-07-13 的记录也显示"创建管理员级凭据"会被权限分类器拦截，且属于本会话明令禁止的操作）。
- 因此以下项本轮**没有**浏览器实测证据，需人工在已登录标签页确认：表格 16 列渲染与列序、漏斗位置、同一订单交替底色、清空筛选恢复、CSV/XLSX 实际下载、320px 无页面级横滚、Console 无红色错误。上述行为的数据与参数层面已由隔离库实证与自动化测试覆盖；纯渲染/样式层面仅有自动化契约测试覆盖。
- 5173 与 8090 均保持运行（5173 PID 28336，8090 PID 27636，均指向 `pjsk_qr_dev`），用户可直接在已登录标签页刷新 `/admin/orders` 完成人工验收。

## 阶段 5A：技术标识全站调查（2026-07-17，完成）

开始前基线：分支 `main`，HEAD `c8e7df0` = `origin/main`；阶段 1～4F、门户重构、付款二维码与迁移 `0020` 等成果均未提交且完整保留；`git diff --check` 无空白错误（仅 `style.css` 的 CRLF 提示）。先只读调查，结论如下。

### 普通用户边界（`internal/query` + 普通用户模板）——本来就是干净的
- `query.User.ID`、`QueryCodeHash`、`Status` 均为 `json:"-"`；`PaymentRecord.ID` 也是 `json:"-"`（仅作内部拼装 Items 用）。
- `query.OrderItem` / `query.PaymentItem` 是专用 DTO，结构体注释明确写了"绝不携带订单号、项目名、内部 id、导入/来源追踪、审计字段"；`orderRow` 被刻意独立出来，避免订单号/项目名被序列化。
- 结论：阶段 3 已经把普通用户边界做到位，**5B 无需改代码**，只需补齐契约测试锁死它。

### 秘密字段
- 全仓库检索 `json:"...hash|token|secret|password|query_code..."`：命中的 `admin.Password`、`query.QueryCode`、`bindcode.BindToken`、`New/ConfirmQueryCode` **全部是入站请求结构体**，没有任何一个是响应。
- 管理员用户列表只下发 `has_query_code`（布尔状态）与 `query_code_updated_at`，不下发查询码或其哈希；页面文案也写明"管理员不能查看原查询码""明文只会用于本次保存，页面不会显示或记录"。
- 结论：无秘密外泄。

### 管理员主视图
- 订单主表（阶段 4F 后）：无批次 ID/SKU/SHA/来源定位；`order_id` 仅作 key 与详情导航。
- 导入历史主表：文件名/状态/上传/确认/工作表数/问题数/写入结果/总金额，均为业务字段；`:key="item.id"` 只是 key。
- 用户主表：CN/查询权限/查询码状态/创建时间/最后登录/订单数/金额，无技术字段。
- **发现问题 1**：导入历史（`admin-import-history`，一个*列表*页）底部挂了一个 `technical-section`，展示"导入记录 ID / 文件 SHA"。这违反"技术字段只在管理员详情页出现"。且导入详情页（`/admin/imports/:id`）已经包含同样的"导入记录 ID"与"文件 SHA"，故从列表页移除不损失任何排查能力。

### 管理员详情技术区
共 5 处 `technical-section`：付款详情、用户详情、订单详情、导入历史（见上，应移除）、导入详情；另有付款二维码卡片的 `technical-panel`。
- **发现问题 2**：只有订单详情（上一轮 4F 加的）带"仅供技术排查"，其余都没有；`<summary>` 文案是"查看技术标识"，未统一为"技术标识"。
- **发现问题 3**：付款二维码卡片的技术区 `<summary>` 是"技术详情（仅管理员可见）"，既未统一标题也无"仅供技术排查"，且混排了格式/大小/更新管理员/更新时间等业务字段与 SHA-256。
- 5 处 `<details>` 均未写 `open`，默认收起（含移动端）——这一条本来就满足。

### 导出边界——已符合，只缺测试
- 订单明细导出表头：`CN, 已付, 小计, 剩余, 付款状态, 谷子名称, 角色, 分类, 数量, 单价, 显示名称, 项目, 订单号, 来源文件, 来源 Sheet, 来源位置` —— 业务字段在前，三个技术/审计字段（来源文件/Sheet/位置）已经在最后，中文表头，名称清楚。
- 用户导出：`CN, 订单总金额, 有效已付总额, 剩余待付总额, 显示名称, 查询码状态, 用户状态, 订单数, 创建时间` —— "查询码状态"是状态而非查询码本身，无秘密。
- 付款导出：全为业务/审计字段，无技术标识。
- 普通用户页面没有任何导出入口。

### 错误信息
- 后端统一用 `logsafe.Category(err)` 写日志，返回给前端的是固定文案（业务校验为中文，未知故障为 `internal server error`），不外泄 SQL、表名、堆栈或绝对路径。
- 保留观察项：500 的兜底文案 `internal server error` 是英文。把它中文化会牵动 orders/query/export/payments/users 等多个包的公共 `writeError`，超出本阶段"技术标识隐藏"的范围，本轮不改，仅以测试锁定"不外泄 SQL/路径/堆栈"，并记为后续可选项。

### 阶段 4F-8 相关
- 主表当前 16 列，需删除"用户名称、项目名称、谷子种类"三列并同步移除对应筛选，改为 13 列；`min-width` 当前 1460px，删列后需重估。
- 后端 `project`/`category` 的查询与 facet 能力按指令保留（导出与其他调用方仍可用），仅前端订单页不再请求/展示/统计。

## 阶段 4F-8A：管理员订单主表列精简（2026-07-17，完成）

- 主表由 16 列改为**严格 13 列**，顺序：CN、谷子名称、谷子系列、谷子角色、数量、单价、明细总金额、已付金额、未付金额、订单状态、付款状态、创建时间、查看详情。
- 删除「用户名称、项目名称、谷子种类」三列：`<th>` 与 `<td>` 一并移除，不留空表头、不留占位单元格、不用 CSS 隐藏；`item.display_name`/`item.project_name`/`item.category` 不再被渲染；`colspan` 由 16 改为 13。
- 同步移除项目名称与谷子种类的筛选：`orderFilterState` 的 `valueColumns` 改为 `['cn','item','series','role','status','payment_status']`。由于 `buildFilterParams` 只遍历这份状态，订单页因此**不再请求也不再统计** `project`/`category` 的 facets。后端 `project`/`category` 的查询与 facet 能力按指令**保留**（导出与其他调用方不受影响）。
- 漏斗分布：CN/谷子名称/谷子系列/谷子角色/订单状态/付款状态为值筛选，数量/明细总金额/已付金额/未付金额为范围筛选，创建时间为日期筛选；**单价与查看详情无漏斗**。
- 表格宽度重估：`min-width` 由 16 列时代的 `1460px` 改为 `1100px`（不机械保留过宽值）；`.cell-wrap` 由 220px 收到 200px，谷子名称/系列/角色继续正常换行、不用省略号掩盖业务内容；数字金额仍 `tabular-nums`、两位小数；页面本身不横滚，仅 `.table-scroll` 内部滚动。
- 数据粒度未回退：一行仍等于一条 `order_items`；无 `array_agg`/`string_agg`/`joinColumnValues`；无 `rowspan=`；同一 CN 仍多行并按订单交替底色；查看详情仍走 `item.order_id`。

## 阶段 5B：普通用户技术字段清理（2026-07-17，完成——本来就干净，改为锁死）

调查确认 `internal/query` 与普通用户模板本就不下发/不展示任何技术标识（阶段 3 已做到位），因此**未改动生产代码**，改为补齐会真正失败的回归测试：

- 新增 `backend/internal/query/technical_boundary_test.go`：不是检查源码文本，而是把 DTO **真正序列化成 JSON 再检查键名**——`json:"-"` 一旦被误删就会立刻失败。
  - `TestUserDTOsNeverSerializeTechnicalIdentifiers`：给 `User`/`OrderItem`/`PaymentItem`/`PaymentRecord` 填满非零值（避免 omitempty 掩盖问题），断言编码结果不含 id/order_id/product_id/import_batch_id/sku/sha/file_hash/source_*/order_no/project_* 等 19 个技术键，同时确认 `cn_code` 等业务字段仍在。
  - `TestUserResponseCarriesNoInternalUUIDs`：整体编码一个完整 `/api/query/orders` 响应，断言原始 JSON 文本里**不出现**内部订单 UUID、付款 UUID 与查询码哈希（无论嵌套多深），业务内容仍完整。
- 前端新增 `tests/technical-identifiers.test.mjs` 中的普通用户部分：`query-orders`/`query-payment`/`query-security` 模板不含技术标识、无 `technical-section`、无导出入口。

## 阶段 5C：管理员主视图技术字段清理（2026-07-17，完成）

- 订单主表：断言不含 import_batch_id/sku/file_hash/source_*/order_no，且不渲染 `item.order_id`、`item.item_id`（仅作 key 与详情导航）。
- **导入历史列表页移除技术标识区**：该页是*列表*视图，却挂着展示「导入记录 ID / 文件 SHA」的 `technical-section`，违反"技术标识只在管理员详情页出现"。导入详情页（`/admin/imports/:id`）已包含同样的两个标识，因此移除不损失任何排查能力；同时删除随之失效的 `importHistoryTechnicalIdentifiers()`，不留死代码。
- 用户列表、付款记录、导入历史主表复核：均为业务字段，无 UUID/批次 ID/SKU/SHA/幂等键/来源定位。

## 阶段 5D：导出与详情边界（2026-07-17，完成）

### 详情技术区统一契约
- 全站 5 个技术区（付款详情、用户详情、订单详情、导入详情、付款二维码卡片）统一为：`<details>` 默认收起（移动端同样收起）、标题统一为**「技术标识」**、紧随其后一行**「仅供技术排查，日常对账与查询无需使用。」**
- 修正前的实际情况：只有订单详情带"仅供技术排查"，其余 4 处都没有；`<summary>` 文案是"查看技术标识"；二维码卡片更是"技术详情（仅管理员可见）"。
- 二维码卡片额外调整：把「格式/大小/更新管理员/更新时间」移出技术区（这些是日常要看的审计事实，不该逼管理员展开排查区才能看到），技术区内只留 SHA-256。新增 `.qr-meta-list` 复用既有 `.qr-tech-list` 样式。
- 订单详情技术区仍是页面最后一块内容（有测试断言其后不再有业务 section）。

### 导出边界（已符合，补测试）
- 未改导出字段：订单明细导出的三个来源审计字段（来源文件/来源 Sheet/来源位置）本就排在业务字段之后；中文表头、名称清楚。按指令"不要贸然删除"，仅归类并用测试固定顺序。
- 新增 `backend/internal/export/technical_boundary_test.go`：直接调用 `orderItemHeaders()` 断言审计字段在最后、业务字段在前、表头非空且不是 id/hash/uuid/key 之类模糊缩写；并断言订单明细与用户导出都不含密码/验证码/恢复令牌/会话令牌/加密密钥，查询码只允许以「查询码状态」出现。（这条原本写在前端用正则匹配 Go 源码，已移到 Go 侧——那里能直接调函数，可靠得多。）

### 错误信息
- 断言 `orders` 处理器统一 `log.Printf(..., logsafe.Category(err))` + 固定文案 `internal server error`，且不存在 `writeError(..., err.Error())` / `http.Error(..., err.Error())`；前端只展示中文兜底文案，不回显 `String(error)` 或 `error.stack`。
- 已记录的观察项：500 兜底文案仍是英文 `internal server error`（不外泄 SQL/路径/堆栈，故不属于本阶段的技术标识泄漏），中文化会牵动多个包的公共 `writeError`，超出本阶段范围，留作后续可选项。

## 阶段 5E：测试与隔离验收（2026-07-17）

### 自动测试（未删除或弱化任何既有测试）
- 前端：`vue-tsc -b` 通过；`vite build` 通过；`node --test tests/**/*.test.mjs` → **tests 100 / pass 100 / fail 0**（订单筛选专项 42 项，其中 4F-8 新增 4 项；技术标识专项新增 12 项）。
- 后端：`go fmt`/`gofmt -l` 干净；`go build ./...` 通过；`go vet ./...` 通过；`go test ./...` → **18 个包全部 PASS**（含 query 与 export 的技术边界新测试）。

### 隔离环境（再次实证）
- 8090 用当前工作树重建后重启（新 PID 10964），`/health` 返回 `{"status":"ok","database":"connected"}`。
- **数据库归属实证**：PID 10964 持有的 postgres 连接本地端口 50111，从 `pg_stat_activity` 反查 `datname = pjsk_qr_dev`；无任何指向正式 `pjsk` 的连接。正式 8080 未访问、未停止、未修改。
- 5173（PID 28336）仍以显式 `VITE_BACKEND_TARGET=http://127.0.0.1:8090` 运行；`vite.config.ts` 的默认值是 8080，故该显式覆盖是必需的。受控浏览器加载 `/admin/orders`，Console 无 error。

### 管理员可视验收：仍然阻塞（与上一轮相同原因）
- Claude in Chrome 扩展本轮再次重试仍为 not connected；受控预览浏览器是全新上下文，`/api/admin/orders` 返回 401。
- 按指令未采取任何绕过：不创建新管理员、不绕过认证、不代填密码。
- 已按指令保持 5173 与 8090 隔离服务运行，并输出人工验收清单（见最终报告）。

## 阶段 6A：用户与账号只读调查（2026-07-17，完成）

基线：分支 `main`，HEAD `c8e7df0` = `origin/main`；阶段 1～5 成果均未提交且完整；`git diff --check` 干净（仅 `style.css` CRLF 提示）。先只读调查，结论如下。

### 1. 当前用户列表接口响应字段
`GET /api/admin/users` → `{items: ListItem[], summary: ListSummary}`。
`ListItem`：`id`、`cn_code`、`display_name`、`has_query_code`、`status`、`order_count`、`total_amount`、`paid_amount`、`remaining_amount`、`created_at`、`query_code_updated_at`、`last_login_at`。
`summary`：`user_count`、`users_with_orders`、`total_amount`、`paid_amount`、`remaining_amount`。

### 2. 分页现状
**没有后端分页**，只有一个 `limit`（由 `Filters.Limit` 传入）。响应无 `page`/`page_size`/`total`/`total_pages`。

### 3. 现有筛选参数
只有两个单值参数：`cn`（对 `cn_code` 与 `display_name` 做 `ilike %…%` 模糊匹配）和 `status`（等值）。无多值、无范围、无日期。

### 4. 页面现在显示的业务字段
主表 10 列：CN（含显示名小字）、查询权限、查询码（已设置/未设置 + 更新时间）、创建时间、最后登录、订单数、订单总金额、已付金额、剩余金额、详情按钮。
顶部有一个旧式筛选表单（CN 输入框 + 状态下拉 + 查询/重置按钮），另有 5 个汇总方框。

### 5. 技术标识
主表未渲染任何技术标识；`user.id` 仅用于 `:key` 与详情导航。`ListItem.id` 会序列化，但不展示——符合"内部 ID 可用于 key 和导航，不得渲染"。未发现查询码原文/哈希、邮箱密文/盲索引、会话 ID、令牌、验证码出现在列表接口或页面。

### 6. 各状态的来源表
- 查询权限：`users.status`（`active`/`disabled`/`merged`，前端 `userStatusLabel` 已映射为 正常/已停用/已合并）。
- 是否设置查询码：由 `coalesce(users.query_code_hash,'') <> ''` 推导为布尔，**只下发布尔**，不下发哈希；另有 `users.query_code_updated_at`。
- 最后登录：`users.last_query_login_at`，无记录时后端给空串。
- 订单数/总金额/已付：`paidByItemCTE` 里的 `user_totals`——由 `orders` + `order_items`（`revoked_at is null`、`o.status <> 'cancelled'`）左连 `paid_by_item`（`payment_items` join `payments`，且 `filter (where p.status='approved')`）聚合而来；未付 = `greatest(total - paid, 0)`。**已撤销付款本就不计入**。
- 是否绑定恢复邮箱：`user_recovery_emails`（迁移 `0016`），每用户至多一条"当前"记录（唯一索引 `where invalidated_at is null`），`status` 为 pending/verified/disabled。既有语义 `has_recovery_email` = 存在 `invalidated_at is null` 的当前记录，与 status 无关；本阶段沿用同一语义，不发明新定义。

### 7. 是否需要迁移
**不需要**。所需字段（`users.status`、`query_code_hash`、`last_query_login_at`、`user_recovery_emails.invalidated_at`）均已存在，且 `user_recovery_emails_user_id_index`、`users` 相关索引也在。本阶段不执行任何迁移。

### 8. 可直接复用的阶段 4 组件
`ColumnFilterButton.vue`（浮层定位/外部点击/Esc/移动端底部面板/键盘聚焦）、`ColumnValueFilter.vue`（搜索/全选/多选/空白/每值数量/取消/确定/加载与错误重试/候选分页）、`ColumnRangeFilter.vue`、`ColumnDateFilter.vue`、`filters/columnFilters.ts`（状态与 URL 编码）**全部可直接复用**，无需改动其筛选语义。

### 需要注意的两个设计点（实施时处理）
1. **汇总方框会被分页破坏**：现有 `summary` 是在 Go 里"把返回的行加起来"算出来的。一旦分页，它就会变成"当前页的汇总"，与"结果：共 N 位用户"自相矛盾。必须改为在 SQL 里对**完整筛选结果**聚合。
2. **金额目前在 CTE 里就 `::float8`**：范围比较会落在浮点数上。将按订单模块的做法保持 `numeric` 到比较之后，只在最外层 select 转 `float8`。
3. `users.Filters` 被 `export` 包复用（`users.Filters{CN, Status, Limit}`），改结构时必须同步更新导出，避免破坏既有导出行为。
4. 页面 h2 目前是"用户管理"，而门户模块名与 `adminModuleTitle` 都是"用户与账号"——按指令统一为"用户与账号"。

## 阶段 6B：用户列表分页与筛选后端（2026-07-17，完成）

新增 `backend/internal/users/filters.go`，与订单模块同构（同样的 `BadRequestError`、`argList`、`baseCTE`、`buildConditions(skipColumn)` 结构），便于后续第 7 阶段继续复用。

- **`baseCTE`：隐私在源头收口。** `base` 视图里查询码只以 `case when coalesce(u.query_code_hash,'') <> '' then 'yes' else 'no' end` 形式存在，恢复邮箱只以 `case when re.user_id is not null then 'yes' else 'no' end`（left join `user_recovery_emails ... and re.invalidated_at is null`，沿用既有 `has_recovery_email` 语义）。`query_code_hash` 本身、`encrypted_email`、`email_lookup_hash` **从不出现在 select 列表里**，因此后续无论怎么改 DTO 都泄漏不了。
- **金额保持 numeric**：把原先在 `user_totals` 里就 `::float8` 的写法改为保持 `numeric`，只在最外层 select 转 `float8`——比较不再落在浮点数上。仍只统计 `approved` 付款、排除 `cancelled` 订单，未付 = `greatest(total - paid, 0)`。
- **多值筛选**：`cn`/`name`/`status`/`has_query_code`/`has_recovery_email` 全部改为重复参数 + `= any($n::text[])` 等值匹配。布尔列用 `yes`/`no`（URL 里可读，且与浮层显示一一对应）。
- **范围筛选**：`order_count_min/max`（整数）、`total_min/max`、`paid_min/max`、`unpaid_min/max`（小数），全部 `::numeric`。
- **日期筛选**：`last_login_from/to`、`created_from/to`；`_to` 传日期时自动推到次日零点，所以"到 7-17"包含 17 号当天。
- **空白最后登录**：`last_login_blank=1` → `b.last_query_login_at is null`，可精确筛出"从未登录"的用户。
- **400 校验**：非法数字、负数、小数订单数、`min > max`（四个范围各自校验）、起始日期晚于结束日期、非法状态/布尔值、`page<1`、`page_size<1`、`page_size>200` 全部返回中文 400，绝不做无界查询。`page_size` 超上限直接拒绝而非静默截断。
- **分页**：`page`/`page_size`（默认 50，上限 200；25/50/100/200 均可用），响应新增 `page`/`page_size`/`total`/`total_pages`。
- **修掉一个会被分页引入的真实缺陷**：原 `summary` 是在 Go 里把已扫描的行加起来算的。一旦分页，它就会变成"当前页的汇总"，与"结果：共 N 位用户"自相矛盾。改为 `buildSummaryQuery` 在 SQL 里对**完整筛选结果**聚合，并有测试断言 count/summary 与列表使用逐字相同的 WHERE、且都不带 limit/offset。
- **导出同步**：`users.Filters` 被 export 包复用，故 `loadUsers` 改用同一个 `FiltersFromQuery`（丢弃 page/page_size），并新增 `ExportUsers(ctx, filters, maxRows)`；用户导出因此免费获得多值/范围/日期筛选，且与列表口径一致，仍保留既有 50000 行上限。
- `ListItem` 新增 `has_recovery_email` 布尔字段。

## 阶段 6C：用户 facets 接口（2026-07-17，完成）

新增 `backend/internal/users/facets.go` 与路由 `GET /api/admin/users/facets`。

- 参数：`column`、`search`、`facet_page`、`facet_page_size`（默认 50、上限 200）+ 其他所有已生效筛选。
- 响应：`values[{value,label,count,blank}]`、`total`、`blank_count`、`facet_page`、`facet_page_size`、`total_pages`、`has_more`。
- **计数口径是用户行数**（`count(*)`）："已设置 12" 就是 12 位用户，勾选后列表恰好返回 12 行。
- 作用域：应用其他列筛选、**排除当前列自身值筛选**（否则取消勾选后该值就再也回不来）、保留范围与日期筛选（含 `last_login_blank`）。
- 空白：`<column>_blank=1`；普通空串与纯空白参数忽略；空白候选排在最后。
- 标签中文化：状态→正常/已停用/已合并，查询码→已设置/未设置，恢复邮箱→已绑定/未绑定，空白→(空白)。
- 安全：列名只从固定白名单查表，绝不拼接；`id`、`query_code_hash`、`encrypted_email`、`email_lookup_hash`、`session_id`、`recovery_token` 等一律拒绝；搜索词参数化并转义 `%`/`_`/`\`。
- **路由顺序**：`/api/admin/users/facets` 注册为精确路径，排在 `/api/admin/users/` 前缀之前，ServeMux 优先匹配更长的精确模式，因此不会被详情 handler 抢走（详情本身还有 `isUUIDLike` 兜底）。已加路由测试。

### 后端测试（本阶段新增，均通过）
`filters_sql_test.go`（多值/范围/日期/空白/去重/非法参数 19 例/分页偏移/count 与 summary 同条件/导出无分页/注入防护与占位符连续性）、`facets_test.go`（用户行计数/应用其他列/排除自身列/保留范围与日期/搜索转义/候选分页与上限/空白排序/中文标签/拒绝秘密列/端点 400 与透传）、`privacy_boundary_test.go`（真实序列化后不含 query_code_hash、encrypted_email、email_lookup_hash、bcrypt 前缀等，两个安全列必须是布尔）、`api/user_facet_routes_test.go`（静态路由已注册且受鉴权保护）。
`go fmt`/`go vet`/`go build` 干净；`go test ./...` 18 个包全部 PASS。

## 阶段 6D：前端 WPS 表头筛选接入（2026-07-17，完成）

- 删除旧的顶部筛选表单（CN 输入框 + 状态下拉 + 查询/重置）与 `adminUserFilters` 单值状态。
- 页面 h2 由「用户管理」改为「用户与账号」，与门户模块名和 `adminModuleTitle` 一致。顶部三行：标题+说明 / 导出与刷新 / 结果数、页码、每页数量、清空全部筛选。结果文案「结果：共 N 位用户」。
- 主表 12 列，顺序：CN、用户名称、查询权限、查询码、恢复邮箱、订单数量、总金额、已付金额、未付金额、最后登录时间、创建时间、查看详情。`user.id` 只作 `:key` 与详情导航，不渲染。
- 值筛选（`ColumnValueFilter`）：CN、用户名称、查询权限、查询码、恢复邮箱。范围筛选（`ColumnRangeFilter`）：订单数量、总金额、已付金额、未付金额。日期筛选（`ColumnDateFilter`）：最后登录时间、创建时间。查看详情列无漏斗。
- **`ColumnDateFilter` 扩展了可选的空白项**（`allow-blank` + `blank-label`），最后登录时间用它提供「从未登录」，并与日期区间互斥（勾选后日期输入禁用——没有日期的行不可能落在区间内，禁用比静默返回空结果诚实）。创建时间不传 `allow-blank`，因此不渲染复选框，订单页的创建时间筛选行为完全不变。`DateSelection` 增加 `blank` 字段，`buildFilterParams` 据此发出 `<column>_blank=1`。
- **facet 类型改名**：`OrderFacetValue/OrderFacetResponse` → `ColumnFacetValue/ColumnFacetResponse`（现在被两个页面共用，名字不该带 order）。用户 facets 接口按指令用 `facet_page`/`facet_page_size` 命名分页，因此在 `loadAdminUserFacets` 里适配成浮层读取的 `page`/`page_size` 形状。
- 汇总方框保留，但改用后端对**完整筛选结果**的聚合，不再由前端加总当前页。
- 导出改为携带完整筛选参数（`adminUserFilterParams()`），不带分页。
- 新增 CSS：`.user-table table { min-width: 1080px }`（12 列，比订单表窄；仅表格容器内部横向滚动）、`.column-filter-option--blank`。

## 阶段 6E：测试与隔离验收（2026-07-17）

### 自动测试（未删除或弱化既有测试）
- 前端：`vue-tsc -b` 通过；`vite build` 通过；`node --test tests/**/*.test.mjs` → **tests 120 / pass 120 / fail 0**（新增 `user-column-filters.test.mjs` 18 项；`technical-identifiers.test.mjs` 增至 13 项）。改共用组件与编码器后，订单页 100 项既有测试全部仍通过。
- 后端：`go fmt`/`go vet`/`go build` 干净；`go test ./...` **18 个包全部 PASS**。

### 隔离环境（再次实证）
- 8090 用当前工作树重建后重启（新 PID 29088），`/health` 正常；**从 `pg_stat_activity` 反查**其连接（本地端口 59003）`datname = pjsk_qr_dev`，无任何指向正式 `pjsk` 的连接。5173（PID 28336）仍以显式 `VITE_BACKEND_TARGET=http://127.0.0.1:8090` 运行。正式 8080（PID 21248）未访问、未停止、未修改。

### 用真实隔离数据验证（临时只读测试，验证后已删除，未留在仓库）
直接调用真实的 `ListUsers`/`UserFacets`/`ExportUsers` 连 `pjsk_qr_dev` 执行：
- 默认加载：`total=1` 位用户，`page=1/1`；汇总 用户 1 / 有订单 1 / 总额 145.00 / 已付 25.00 / 未付 120.00 —— 与阶段 4F 的订单口径一致，且**汇总用户数 == 结果总数**（分页后仍自洽）。
- 用户行：`CN=测试CN01 | 名称=验收测试用户（测试数据） | 权限=active | 查询码=true | 恢复邮箱=false | 订单=1 | 最后登录=2026-07-17T11:23:19Z`。
- 值筛选逐列验证：CN→1；查询权限→1；查询码=已设置→1、未设置→0；恢复邮箱=已绑定→0、未绑定→1（与该用户实际状态一致）；每次汇总都与总数一致。
- 范围：订单数>=1→1，返回行的 order_count 均 >=1。
- 组合：CN + 查询权限→1。
- 空白：从未登录→0（该用户 11:23 登录过，正确）。
- facets：五列的候选计数合计均 == 用户总数 1；标签中文（正常 / 已设置 / 未绑定 / 测试CN01 / 验收测试用户（测试数据））；`blank_count`、`total_pages` 均返回。勾选 CN 后再取 CN facet 仍返回 1 个候选 —— **排除自身列生效**。
- 分页：`page=1&page_size=25` → `total=1 total_pages=1`。
- 导出：传 `page=2&page_size=1` 时导出仍为 1 行 == 筛选总数 —— **导出忽略分页**。

### 受隔离数据限制的动态项
隔离库只有 1 位用户，因此多值勾选、多页翻页、`(空白)` 用户名称候选、从未登录用户等无法动态触发；这些由后端单元测试与前端契约测试覆盖。未为了验收修改测试数据。

### 管理员可视验收：仍然阻塞（与前两轮同因）
- Claude in Chrome 扩展本轮再次重试仍为 not connected；受控预览浏览器为全新上下文，无管理员会话。
- 按指令未采取任何绕过：不创建新管理员、不绕过认证、不代填密码。5173 与 8090 隔离服务保持运行，人工验收清单见最终报告。受控浏览器加载 `/admin/users` 时 Console 无 error。

## 阶段 7A：付款记录只读调查（2026-07-17，完成）

基线：分支 `main`，HEAD `c8e7df0` = `origin/main`；阶段 1～6 成果均未提交且完整；`git diff --check` 干净（仅 `style.css` CRLF 提示）。先只读调查，结论如下。

### 1. 当前付款列表接口字段
`GET /api/admin/payments` → `{items: PaymentListItem[]}`（**无 summary**）。
`PaymentListItem`：`id`、`cn_code`、`display_name`、`amount`、`principal_amount`、`fee_amount`、`payable_amount`、`total_amount`、`payment_method`、`status`、`paid_at`、`created_by`、`note`、`payment_item_count`、`created_at`、`voided_at`、`voided_by`、`void_reason`。

### 2. 分页现状
**没有后端分页**，只有 `limit`。响应无 `page`/`page_size`/`total`/`total_pages`。

### 3. 现有筛选参数
`cn`（对 `cn_code`/`display_name` 做 `ilike %…%`）、`payment_method`（`lower(...) = lower(...)`）、`status`（等值）、`paid_from`/`paid_to`、`principal_min/max`、`fee_min/max`、`payable_min/max`、`limit`。均为单值；**没有撤销日期范围，也没有撤销空白筛选**。

### 4. 一行的准确含义
一行 = 一条 `payments` 记录（一次付款）。SQL 按 `p.id` 分组只是为了 `count(pi.id)` 得到"关联明细数量"，不改变粒度。

### 5. 金额字段来源（数据库真实落库值）
- 本金 = `payments.submitted_amount`（迁移 `0011` 注释明确写着"submitted_amount is the order principal"）。
- 手续费 = `payments.fee_amount`（`0011` 新增）。
- 实付金额 = `payments.payable_amount`（`0011`：the amount actually paid by the user）。
- DTO 里 `amount` / `principal_amount` 是 `submitted_amount` 的两个别名，`total_amount` 是 `payable_amount` 的别名（`item.PrincipalAmount = item.Amount`、`item.TotalAmount = item.PayableAmount`）。**正式口径取 `principal_amount` / `fee_amount` / `payable_amount`**，全部直接读库，前端不重算。

### 6. 状态语义
`payments.status` ∈ `submitted` / `approved` / `rejected` / `cancelled` / `voided`（`0001` 建表 + `0010` 放宽约束加入 `voided`）。`0010` 同时新增 `voided_at`、`voided_by_admin_id`。中文映射（`export/labels.go`）：approved→已交肾、voided→已撤销、submitted→待处理、rejected→已驳回、cancelled→已取消。

### 7. 撤销该用独立筛选还是状态表达
**应由 `status=voided` 表达。** 现状是页面上既有"付款状态"下拉（含"已撤销"），又有一个"是否撤销"下拉——后者 `@change` 直接写 `paymentFilters.status`，两个控件抢同一个状态、互相覆盖，是重复且会互相打架的设计。按指令删除"是否撤销"，只保留状态列；另新增"撤销时间"日期列并支持空白（空白=未撤销）。

### 8. 付款方式中文映射
`alipay`→支付宝、`wechat`→微信、`bank`→银行转账、`cash`→现金、`other`→其他（`export/labels.go`）。`normalizePaymentMethodFilter` 会把 `wx`/`weixin`/`微信`、`zhifubao`/`支付宝` 等别名归一到规范值，列表读取时也会对存量值归一。

### 9. 导出是否复用当前筛选
**没有完全复用——这是一个真实的口径不一致。** `export.loadPayments` 只传 `cn`/`payment_method`/`status`/`paid_from`/`paid_to` + `Limit`，**丢掉了本金/手续费/实付金额三个范围**。也就是说页面按金额范围筛过之后点导出，导出的行数会多于页面显示。本阶段一并修正为复用同一个筛选解析器。

### 10. 是否需要迁移
**不需要。** `submitted_amount`、`fee_amount`、`payable_amount`、`status`（含 `voided`）、`voided_at`、`voided_by_admin_id`、`created_by`/`approved_by` 全部已存在（`0001`/`0010`/`0011`）。本阶段不执行任何迁移。

### 其他注意点
- 路由现有 `/api/admin/payments/cn`、`/unpaid`、`/`（详情）、``（列表）。`/api/admin/payments/facets` 需注册为精确路径，ServeMux 优先匹配更长精确模式，不会落入详情 handler。
- `/admin/finance/payments` 页面同时承载"录入付款"面板（业务功能），本阶段只改下方的付款记录列表，不动录入面板。
- 可直接复用阶段 4/6 的 `ColumnFilterButton`/`ColumnValueFilter`/`ColumnRangeFilter`/`ColumnDateFilter`（含第 6 阶段为"从未登录"加的 `allow-blank`，正好可用于"撤销时间"的未撤销空白）与 `filters/columnFilters.ts`；后端可照搬 `users` 包的 `filters.go`/`facets.go` 同构结构。

## 阶段 7B：付款列表分页与筛选后端（2026-07-17，完成）

新增 `backend/internal/payments/filters.go`，与订单/用户模块同构（同样的 `BadRequestError`、`argList`、`baseCTE`、`buildConditions(skipColumn)`）。

- **`baseCTE`：一行 = 一条 `payments` 记录。** 金额直接读落库列，**从不重算**：本金 `p.submitted_amount`、手续费 `p.fee_amount`、实付金额 `p.payable_amount`。微信历史手续费因此保留下单时实际收取的值，不按当前费率推算；支付宝通常存 0.00。金额保持 `numeric` 到比较之后，只在最外层转 `float8`。
- **撤销付款金额口径**：voided 记录照常显示原本金/手续费/实付金额，状态列显示"已撤销"；SQL 不做 `case when voided then 0`，也没有 `filter (where status='approved')` 把它当已付汇总。状态是普通列，approved 与 voided 始终可区分——列表筛选与导出都不会把 voided 当 approved。
- **多值筛选**：`cn`/`payment_method`/`status`/`created_by`（录入管理员）改为重复参数 + `= any($n::text[])`。付款方式在解析时归一（`Alipay`/`微信`→`alipay`/`wechat`），与库里存的规范值一致，否则会静默匹配不到。
- **范围筛选**：`principal_min/max`、`fee_min/max`、`payable_min/max`，全部 `::numeric`。
- **日期筛选**：`paid_from/to`、`voided_from/to`；`_to` 传日期时推到次日零点（含当天）。**朴素时间锚定 Asia/Shanghai**——沿用原 `normalizeChinaTimestampParam` 的行为，避免依赖数据库会话时区导致筛选日整体偏移 8 小时；带偏移量的值按原样保留。原 `normalizeChinaTimestampParam`/`validateOptionalPaymentTime` 已被 `FiltersFromQuery` 取代并删除（其中文时区语义由 `TestFilterTimestampsAnchorToChinaTime` 继续覆盖，未弱化测试）。
- **撤销时间空白**：`voided_blank=1` → `b.voided_at is null`，即"未撤销"。
- **400 校验**：非法金额、负数、`min>max`（三个范围各自）、起始晚于结束日期、非法状态/方式、`page<1`、`page_size<1`、`page_size>200` 全部中文 400；`page_size` 超上限直接拒绝而非静默截断。
- **分页**：`page`/`page_size`（默认 50、上限 200；25/50/100/200 可用），响应新增 `page`/`page_size`/`total`/`total_pages`。计数用与列表逐字相同的 WHERE、不带 limit/offset。
- **DTO**：`PaymentFilters` 全面改为多值 + 范围 + 日期 + 撤销 + 分页；`PaymentListItem` 不变（其 `amount`/`total_amount` 仍作 principal/payable 别名兼容既有消费者）；主表只渲染业务列，`id` 仅作 key 与详情导航。

## 阶段 7C：付款 facets 接口（2026-07-17，完成）

新增 `backend/internal/payments/facets.go` 与路由 `GET /api/admin/payments/facets`。

- 参数：`column`、`search`、`facet_page`、`facet_page_size`（默认 50、上限 200）+ 其他所有已生效筛选。
- 响应：`values[{value,label,count,blank}]`、`total`、`blank_count`、`facet_page`、`facet_page_size`、`total_pages`、`has_more`。
- **计数口径是付款记录行数**（`count(*)`）："已撤销 3" 就是 3 条付款，勾选后列表恰好 3 行。
- 作用域：应用其他列筛选、**排除当前列自身值筛选**、保留范围与日期（含 `voided_blank`）。空白 `<column>_blank=1`；空/纯空白忽略；空白候选排最后。
- 标签中文化：状态→已交肾/已撤销/待处理/已驳回/已取消，方式→支付宝/微信/银行转账/现金/其他，空白→(空白)。
- 安全：列名只从固定白名单查表；`id`/`idempotency_key`/`user_id`/`screenshot_storage_path`/`note` 等一律拒绝；搜索词参数化并转义 `%`/`_`/`\`。
- **路由顺序**：`/api/admin/payments/facets` 注册为精确路径，排在 `/api/admin/payments/`（详情）前缀之前，ServeMux 优先匹配精确模式；详情 handler 另有 id 校验兜底。已加路由测试。

## 阶段 7D：前端 WPS 表头筛选接入（2026-07-17，完成）

- 删除付款记录页顶部整块筛选表单、"高级筛选"展开区，以及 `paymentFilters`/`paymentAdvancedOpen`/`paymentQueryString` 旧状态与函数。
- **删掉了一个真实的重复控件**：旧页面同时有"付款状态"下拉和"是否撤销"下拉，两者都写同一个 `status`，互相覆盖。现在只保留付款状态列（含"已撤销"），撤销与否由 status=voided 表达；另新增"撤销时间"日期列，`allow-blank` + `blank-label="未撤销"`。
- 主表 10 列，顺序：CN、本金、手续费、实付金额、付款方式、付款状态、付款时间、录入管理员、撤销时间、查看详情。备注移到详情页，不进主表。`payment.id` 仅作 key 与详情导航，不渲染。
- 值筛选：CN、付款方式、付款状态、录入管理员。范围筛选：本金、手续费、实付金额。日期筛选：付款时间、撤销时间。查看详情列无漏斗。
- 顶部三行：标题+说明 / 导出（付款 Excel/CSV、未付明细 Excel/CSV）+刷新 / 结果数、页码、每页、清空全部筛选。结果文案"结果：共 N 条付款记录"。
- facet 类型复用共用的 `ColumnFacetResponse`；付款 facets 接口用 `facet_page`/`facet_page_size` 命名分页，在 `loadPaymentFacets` 里适配成浮层读取的形状。
- **导出修正**：旧导出只传 cn/method/status/paid_*，丢掉了三个金额范围，导致金额筛选后导出行数多于页面。现改为 `paymentFilterParams()` 携带完整筛选、不带分页；后端 `loadPayments` 也改用同一个 `FiltersFromQuery` + 新增 `ExportPayments(ctx, filters, maxRows)`，与列表逐行一致，仍保留 50000 行上限。
- 新增 CSS：`.payment-records-table table { min-width: 1000px }`（10 列，仅容器内部横向滚动）。

## 阶段 7E：测试与隔离验收（2026-07-17）

### 自动测试（未删除或弱化既有测试）
- 后端：`go fmt`/`go vet`/`go build` 干净；`go test ./...` **18 个包全部 PASS**。新增 `payments/filters_sql_test.go`（多值/别名归一/范围/落库金额不重算/voided 金额保留/中国时区锚定/撤销日期/未撤销空白/去重/非法参数 18 例/分页偏移/count 同条件/导出无分页/注入防护/主表无技术标识）、`payments/facets_test.go`（付款行计数/应用其他列/排除自身列/保留范围与日期/搜索转义/候选分页与上限/空白排序/中文标签/拒绝技术列/端点 400 与透传）、`api/payment_facet_routes_test.go`（静态路由已注册且受鉴权）。既有 `TestNormalizeChinaTimestampParam` 改为 `TestFilterTimestampsAnchorToChinaTime` 指向新解析器，语义未弱化。
- 前端：`vue-tsc -b` 通过；`vite build` 通过；`node --test tests/*.test.mjs` → **tests 139 / pass 139 / fail 0**（新增 `payment-column-filters.test.mjs` 19 项；阶段 4F/6 既有断言全部仍通过）。

### 隔离环境（再次实证）
- 8090 用当前工作树重建后重启（新 PID 2688），`/health` 正常；**从 `pg_stat_activity` 反查**其连接（本地端口 58371）`datname = pjsk_qr_dev`，无任何指向正式 `pjsk` 的连接。5173（PID 28336）经实测 `GET /api/admin/payments` 命中 8090 访问日志，链路为 浏览器→5173→8090→`pjsk_qr_dev`。正式 8080（PID 21248）未访问、未停止、未修改。

### 用真实隔离数据验证（临时只读测试，验证后已删除，未留在仓库）
直接调用真实的 `ListPaymentRecords`/`PaymentFacets`/`ExportPayments` 连 `pjsk_qr_dev` 执行：
- 默认加载：`total=2` 条付款，`page=1/1`。两条正是隔离库既有的一条 approved、一条 voided：
  - `测试CN01 | 本金 20.00 | 手续费 1.00 | 实付 21.00 | 微信 | 已撤销 | 撤销时间 2026-07-17T05:38:33Z | 录入 qa_admin`
  - `测试CN01 | 本金 25.00 | 手续费 0.00 | 实付 25.00 | 支付宝 | 已交肾 | 未撤销 | 录入 qa_admin`
- **金额口径**：voided 那条照常显示 20/1/21，未被清零；微信手续费保留落库的 1.00，未按当前费率重算；支付宝手续费为存储的 0.00。
- 状态筛选：approved→1、voided→1，二者不混。未撤销空白筛选→1（仅 approved 那条）。
- 值筛选：CN→2、付款方式→1（探针 alipay）、录入管理员→2；本金范围 >=0→2。
- facets（计数按付款行数，标签中文）：`cn 测试CN01=2`；`payment_method 支付宝=1, 微信=1`；`status 已交肾=1, 已撤销=1`；`created_by qa_admin=2`；四列计数合计均 == 付款总数 2；勾选 status=approved 后取 status facet 仍返回 2 个候选，**排除自身列生效**。
- 分页：`page_size=25` → `total=2 total_pages=1`。
- 导出：传 `page=2&page_size=1` 时导出仍 2 行 == 筛选总数（**忽略分页**）；`status=voided` 导出仅 1 行且全为 voided。

### 受隔离数据限制的动态项
隔离库只有 2 条付款，多值多选、多页翻页、更多录入管理员/付款方式候选无法动态触发；这些由后端单元测试与前端契约测试覆盖。未为验收修改测试数据。

### 管理员可视验收：仍然阻塞（与前几轮同因）
- Claude in Chrome 扩展本轮再次重试仍为 not connected；受控预览浏览器为全新上下文、无管理员会话，`/api/admin/payments` 返回 401。受控浏览器加载 `/admin/finance/payments` 时 Console 无 error。
- 按指令未采取任何绕过：不创建新管理员、不绕过认证、不代填密码。5173（PID 28336）与 8090（PID 2688）隔离服务保持运行，二者均指向 `pjsk_qr_dev`。

### 人工验收清单（请在已登录的 Chrome 标签页 `http://127.0.0.1:5173/admin/finance/payments` 逐项确认）
1. 顶部无旧筛选表单、无"高级筛选"、无"是否撤销"下拉。
2. 表头严格 10 列且顺序：CN、本金、手续费、实付金额、付款方式、付款状态、付款时间、录入管理员、撤销时间、查看详情。
3. 默认加载 2 条，结果文案"结果：共 2 条付款记录"。
4. CN 漏斗：候选 `测试CN01`（数量 2），勾选后仍 2 条。
5. 付款方式漏斗：候选 支付宝 1 / 微信 1；勾选微信→1 条（那条为已撤销、微信、手续费 1.00）。
6. 付款状态漏斗：候选 已交肾 1 / 已撤销 1；勾选已撤销→1 条且金额仍为 20/1/21。
7. 本金/手续费/实付金额范围：如本金 24–26→只剩支付宝那条。
8. 付款时间日期筛选、撤销时间日期筛选可用。
9. 撤销时间漏斗勾选"未撤销"→只剩 approved 那条。
10. 两列组合（如 付款方式=支付宝 + 付款状态=已交肾）→1 条。
11. 清空全部筛选→恢复 2 条。
12. 每页在 25/50/100/200 间切换正常。
13. 导出付款 CSV/XLSX：在某个筛选下点击，下载文件行数与页面结果一致（金额范围也随导出）。
14. 点击"详情"进入付款详情；详情页本金/手续费/实付金额分列、方式与状态中文、撤销信息清晰、技术标识在底部默认收起的"技术标识"区并标注"仅供技术排查"。
15. 主表不出现付款 ID、幂等键、订单/明细内部 ID、会话 ID 等技术标识。
16. 320px 视口下页面无整体横向滚动，表格仅容器内部滚动，筛选浮层为底部面板。
17. Console 无红色错误。

## 阶段 8A：全站视觉与排版只读调查（2026-07-18，完成）

基线：分支 `main`，HEAD `c8e7df0` = `origin/main`；阶段 1～7 成果均未提交且完整；`git diff --check` 干净（仅 style.css CRLF）。62 个未跟踪/改动文件。只读调查结论：

### 字体系统现状（`style.css` 第 3084 起「统一字体层级」）
- 已有令牌：`--fs-page-title:24 / --fs-section-title:17 / --fs-value:15 / --fs-label:13 / --fs-hint:12 / --fs-amount:16`；字重 `--fw-*`；颜色 `--color-title/value/label/hint/amount`。层级方向正确：**内容(15) > 标签(13)**，满足"内容不小于标签"；页面主标题(24) > 区块标题(17) > 内容(15)。
- **发现的核心问题**：`.page-heading h2` 用的是 `--fs-section-title`（17px），但 `.page-heading` 是管理员 WPS 页（订单/用户/付款）的**页面主标题**容器——于是这些页的主标题只有 17px，仅比内容(15)大 2px，正是用户说的"标题没有明显大于内容"。**修复**：`.page-heading h2` 改用 `--fs-page-title`(24)。
- **不协调来源**：普通用户"我的订单"里的 `订单明细`、`订单 N`（第 4139/4158 行）是**裸 `<h2>`**，不在 `.panel__header` 内，因此拿浏览器默认字号（约 24px 粗体），而 `付款汇总`(在 `.panel__header` 内)=17px。同页 h2 一个约 24 一个 17，正是"大小关系不协调"。`选择付款方式` 等裸 `<h3>` 同理。**修复**：把这些块标题纳入 `t-section-title` 令牌。
- **字体不设 `font-synthesis`**：根节点 `font-synthesis: none`，浏览器不合成粗/斜体，故不存在真正的伪斜体；用户所述"歪/倾斜"更可能是上面裸 h2 尺寸失控 + 中文回退字重不齐造成的观感。无 `transform/skew/oblique`。

### 斜体/歪斜扫描
- 全仓库仅 **1 处** `font-style: italic`：`.column-filter-option__label.is-blank`（筛选浮层里"(空白)"候选标签）。按"禁止斜体"移除，改用颜色弱化区分。无 `skew`/`oblique`。

### 名称统一现状
- 旧名称（用户服务台/返回服务台/谷子管理工作台/返回工作台/返回入口选择）全仓库计数已为 **0**（阶段 2 已处理）。
- 现有：管理员壳层 `← 返回谷子管理中心`（→/admin）；用户子页 `← 返回用户中心`（→/query，唯一一处，同层级一致）；`/query` 与 `/admin` 顶层及登录页 `← 返回系统主页`（→/）。命名一致，无混用。将补测试锁定 0 旧名称。

### 导出按钮与标题分行现状
- 管理员订单/用户与账号/付款记录三页已是阶段 4F/6/7 落地的三行结构：`.page-heading`(标题+说明) / `.page-actions`(导出+刷新) / `.page-resultbar`(结果数/分页/每页/清空)。**标题与导出已分行**，满足 8D。
- 导入历史页（`/admin/data/history`，第 3846 行）仍是旧 `.panel__header` 结构（h2 + 刷新按钮同行），但**该页没有 CSV/XLSX 导出**（仅刷新），不在 8D 强制范围；其布局与 WPS 化留到阶段 9。
- 未付明细导出按钮位于付款记录页的 `.page-actions` 行内，已与标题分行。

### 居中现状（`style.css` 第 2999「全站文字居中」块）
- 已有：`.panel__header { justify-content:center; text-align:center }`、`table th, table td { text-align:center; vertical-align:middle }`、`.page-actions/.page-resultbar { justify-content:center }`、`.page-heading { text-align:center }`。主轴容器 `.app-shell { width:min(1320px, 100%-28px); margin:0 auto }`。基础居中已具备。

### 阶段 8 拟实施（聚焦、可测）
1. `.page-heading h2` → 页面主标题字号(24)。
2. 裸块标题（`.panel h2` / `.panel h3` / `.query-orders-heading h2` / `.query-order-card__heading h2`）纳入区块标题令牌，消除浏览器默认字号造成的不协调；`.page-heading h2` 规则置于其后以保证页面主标题最大。
3. 移除唯一的 `font-style: italic`。
4. 新增 `global-layout-consistency.test.mjs` 锁定：0 旧名称、页面主标题字号 > 区块标题 > 内容、内容 ≥ 标签、无 italic/skew 关键规则、导出页标题与导出区分行、用户/管理员中轴容器存在、表格 th/td 垂直居中、320px 防横滚、阶段 3～7 契约不回退。

## 阶段 8B～8F：字体层级、居中、名称、导出分行统一（2026-07-18，完成）

### 8B 字体层级（`style.css`）
- `.page-heading h2` 由区块标题字号(17)改为**页面主标题字号(24)**——管理员 WPS 页（订单/用户/付款）主标题现明显大于内容。规则置于 `.panel h2` 之后，故对同处 `.panel` 内的 `.page-heading h2` 生效。
- 新增 `.panel h2 / .panel h3 / .query-orders-heading h2 / .query-order-card__heading h2 / .query-pay-block h3` → 区块标题令牌，消除裸 `<h2>/<h3>` 拿浏览器默认字号(约 24px)与 `.panel__header h2(17)` 打架的问题（即"我的订单—订单明细"大小不协调的根源）。层级实测：页面主标题 24 > 区块标题 17 > 内容 15 > 标签 13 > 提示 12；内容>标签成立。
- 移除全站唯一的 `font-style: italic`（筛选浮层"(空白)"标签），改用颜色弱化。根节点保留 `font-synthesis: none`，无伪粗/伪斜。

### 8C 名称统一
- 旧名称计数 0（阶段 2 已处理）；返回标签集合仅 `返回谷子管理中心 / 返回系统主页 / 返回用户中心`，同层级一致、无混用。测试锁定。

### 8D 导出与标题分行
- 订单/用户/付款三页已是 `.page-heading(标题+说明) / .page-actions(导出+刷新) / .page-resultbar(结果/分页/每页/清空)` 三行；标题行内无导出按钮，导出在操作行。测试对三页逐一断言。导入历史页无 CSV/XLSX 导出（仅刷新），不在 8D 强制范围，布局与 WPS 化留阶段 9。

### 8E 居中
- 复用既有：`.app-shell{margin:0 auto}`、`.panel__header/.page-heading/.page-actions/.page-resultbar` 居中、`table th,td{text-align:center;vertical-align:middle}`。

### 8F 测试与真实浏览器验证
- 新增 `frontend/tests/global-layout-consistency.test.mjs`（11 项）：字体层级数值序、WPS 主标题字号、无斜体/歪斜、表格居中、中轴容器、旧名称 0、返回名称一致、导出页三行分行、page-actions/resultbar 居中、320px 防横滚、阶段 3～7 契约。
- 前端：`vue-tsc -b` 通过、`vite build` 通过、`node --test tests/*.test.mjs` → **150/150**。
- 受控浏览器（5173）实测公开首页：`--fs-*` 令牌解析为 24/17/15/13；`getComputedStyle` 扫描全页 **italicCount=0**；桌面 1280 与 320px 均 `docWidth==clientWidth`（无页面级横滚）；Console 无 error。管理员页因受控浏览器无会话未做可视验收，样式为全站 CSS，已由令牌与首页实测覆盖。

## 阶段 9：数据导入中心重排 + 导入历史 WPS 筛选（2026-07-18，完成）

### 9A 调查
- `/admin/data` 已仅有两张模块卡片（Excel 导入 / 导入历史），无上传框/预览表/历史表——满足入口页要求。
- `/admin/data/import` 已分区：上传(`upload-panel`) / 人工审核(`review-panel`) / 确认导入 / 批次列表 / 问题列表，各为独立 `<section class="panel">`，步骤已分块。
- `/admin/data/history` 仍是旧扁平表、无筛选、无分页——这是本阶段的真实缺口。
- `import_batches` 字段齐全（original_filename/file_hash/file_size/sheet_count/total_rows/status/imported_by/confirmed_by/revoked_by/各时间戳/error|warning|notice_count/warnings_accepted/confirm_result(jsonb)/revoke_result(jsonb)）。**无需迁移**。

### 9B/9C 后端（`internal/importpreview/filters.go` + `facets.go`）
- 与订单/用户/付款同构。`baseCTE` 单一事实源：既暴露 `ImportHistoryItem` 扫描器所需的 21 个原始列，又派生筛选列——`issue_count = error+warning+notice`、`written_count = (confirm_result->>'order_item_count')::int`、`total_amount = (confirm_result->>'total_amount')::numeric`。
- 值筛选（多值）：文件名(filename)、状态(status)、上传管理员(uploaded_by)。状态为**自由值列**（候选来自 facets），因导入状态跨迁移增长（previewed/confirmed/reverted…），硬编码白名单会误拒真实值。
- 范围：工作表数、问题数、写入明细数、总金额（`::numeric`）。日期：上传时间(created)、确认时间(confirmed，支持"未确认"空白 `confirmed_blank=1`)。
- 400 校验：非法/负数/小数整型、`min>max`（四范围各自）、起晚于止、非法日期、page/page_size 越界。分页 `page/page_size`(默认 50/上限 200，25/50/100/200)，响应加 `page/page_size/total/total_pages`。count 与 list 用逐字相同 WHERE、不分页。
- **保留 `ImportHistoryItem` 完整**：`listColumns` 严格按扫描器 21 列顺序输出，`ListImports` 继续用 `scanImportHistoryItem`——撤销/详情流不受影响（有 `TestListSelectMatchesScannerOrder` 锁定顺序）。
- facets `GET /api/admin/imports/facets`：`count(*)` 按导入行数、应用其他列/排除自身列/保留范围与日期、`<column>_blank=1`、搜索转义、候选分页；`file_hash`/内部 id 拒绝为筛选列；中文状态标签。路由注册为精确路径，排在 `/api/admin/imports/`(详情) 前，不被详情 handler 抢。

### 9D 前端（`App.vue` + `client.ts` + `style.css`）
- 导入历史改为三行头(标题/刷新/结果) + WPS 表。10 列：文件名、状态、上传管理员、工作表数、问题数、写入明细数、总金额、上传时间、确认时间、查看详情。`item.id` 仅作 key 与详情导航，**主表无 SHA/批次 ID/内部 id**（技术标识仍只在导入详情底部默认收起的"技术标识"区，标注"仅供技术排查"——沿用阶段 5）。确认时间支持"未确认"空白筛选。
- `ImportHistoryResponse` 加分页字段；新增 `ImportFacetResponse`（facet_page 命名，loader 适配为共用 `ColumnFacetResponse`）。新增 `.import-history-table table { min-width:1040px }`（仅容器内部横滚）。

### 9 测试与隔离验证
- 后端新增 `importpreview/filters_sql_test.go`、`importpreview/facets_test.go`（含 endpoint 400/透传 stub）、`api/import_facet_routes_test.go`；`go test ./...` **18 包全 PASS**、`go vet` 干净。
- 前端新增 `import-history-filters.test.mjs`（12 项）：入口页无上传控件、历史页无高级筛选、10 列顺序、WPS 接入、参数/facets/分页接线、筛选重置页码、清空全部、主表无技术字段、详情技术区默认收起、320px 容器内滚、DTO 分页字段、三行结构。`vue-tsc -b` + `vite build` 通过；`node --test tests/*.test.mjs` → **162/162**。
- 隔离验证：8090 重建重启(新 PID 21004)，`pg_stat_activity` 反查连接端口 58727 `datname=pjsk_qr_dev`；5173→8090 经日志确认命中 `/api/admin/imports`。以临时只读测试直连 `pjsk_qr_dev` 调用真实 `ListImports`/`ImportFacets`：隔离库 **import_batches 无数据**（total=0），但含 `confirm_result->>'...'` JSON 抽取的查询**在真实 schema 上执行无误**（schema 有效），空结果与 facets 计数 0 均正确；有数据时的行为由单元测试覆盖。管理员可视验收因 Chrome 扩展未连接、受控浏览器无会话未做（401）；受控浏览器加载壳层 Console 无 error。临时验证文件已删除。正式 8080/`pjsk` 未访问。

## 阶段 10：收款二维码与付款中心中轴统一（2026-07-18，完成）

### 10A 管理员二维码（`/admin/finance/qr-codes`）——调查后确认多数已达标，未过度改动
- `.qr-admin-grid` 已是两列等宽 `repeat(2, minmax(0,1fr))` + `align-items: stretch` → 支付宝/微信卡片**等宽等高**。
- `.qr-card__preview` 固定 `min-height:180px` 且居中；图片与空状态共用同一容器 → **空状态占同样高度**；`.qr-card__head` 居中；标题/方式标题/状态/按钮均在卡片内居中。
- 格式/大小/更新管理员/更新时间在普通 `qr-meta-list`；**SHA-256 仅在默认收起的"技术标识"区**（`<details>` 无 open，标注"仅供技术排查"，沿用阶段 5）。
- 320px 单列（`@media(max-width:560px) .qr-admin-grid → 1fr`）。

### 10B 用户付款中心（`/query/payment`）
- 支付宝/微信按钮 `.query-method-button { flex:1; 居中 }` → 等宽并居中。本金/手续费/本次应付 `.query-amount-grid` 三块等宽，320px 单列。
- **新增修复**：二维码区图片与空状态原本没有统一高度占位，切换付款方式（或已配置/未配置）会导致布局跳动。新增 `.query-qr-slot { min-height:260px; 居中 }` 固定占位槽，把 `<figure>` 与空状态 `<p class="qr-empty">` 都放进去；并给二维码块加 `.query-pay-block--qr { align-items:center }`，使**标题与图片共享同一中轴、空状态与图片占位一致**。清理了 `.query-pay-block h3` 上一条被阶段 8 覆盖的死 `font-size:15px`。
- 说明：本页为只读付款中心，用户侧"确认付款/提交"流程属于阶段 11（收肾记录）新增业务，不在本阶段。

### 10 测试
- 新增 `qr-and-payment-center-axis.test.mjs`（9 项）：二维码卡片等宽等高、图片容器固定居中且空状态同高、头部居中、SHA 仅在收起技术区、320px 单列、方式按钮等宽居中、金额三块等宽、二维码同轴与占位槽、图片居中。
- 纯前端改动：`vue-tsc -b` + `vite build` 通过；`node --test tests/*.test.mjs` → **171/171**。受控浏览器加载公开首页 Console 无 error；二维码与付款中心页需登录会话，样式由 CSS 断言与既有结构覆盖。

## 阶段 11A：付款凭证「收肾记录」——只读调查与设计停止点（2026-07-18）

按指令 11A「先输出并记录数据模型与状态机，再实施」，本节仅调查与设计，**未写任何实现代码**。

### 调查结论
1. **当前用户点击付款只是展示二维码**：`/query/payment` 是只读付款中心（选方式→看应付→看二维码），无任何提交/上传；付款记录由管理员在 `/admin/finance/payments` 录入。
2. **已付金额口径（财务安全锚点）**：用户端 `internal/query/handler.go:576` 与订单端 `internal/orders/filters.go:385` 均为 `sum(payment_items.applied_amount) filter (where payments.status='approved')`。**即：已付仅统计 approved 的 `payments` 经 `payment_items` 分配的金额。** 任何非 approved-payment 的记录都不影响已付。这是"仅提交图片不得增加已付"的天然保证。
3. **approved/voided**：voided 付款不计入已付（stage 7 已验证）；approved 才计入。
4. **文件上传能力已存在且安全（可复用范式）**：`internal/paymentqr` 的收款二维码上传——`MaxImageBytes(5MiB)` + `http.MaxBytesReader`；`ValidateImage` 做**魔数嗅探(`http.DetectContentType`) + 结构解码(PNG/JPEG DecodeConfig、WebP RIFF 解析) + 25MP 解压炸弹上限 + SHA256**；**存储为数据库 BYTEA**（`Image{Data []byte, MimeType, SHA256}`），非文件系统路径；鉴权图片服务带 `X-Content-Type-Options: nosniff`、Content-Type、ETag。
5. **文件路径兼容性 / 备份**：因 QR 选择 DB BYTEA 存储，**天然规避目录穿越、Windows/Linux 路径差异，且随数据库备份一并备份**。`payments.screenshot_storage_path`（0001 遗留列）从未使用——不采用文件系统方案。
6. **最新迁移编号 0020** → 凭证迁移为 **0021**。

### 设计：数据模型（拟新增，未实施）
新表 `payment_submissions`（迁移 0021，独立于 `payments`，**提交本身永不直接影响已付**）：
- `id uuid pk`、`user_id`、`cn_code`、`payment_method`（alipay/wechat）
- `principal_amount`(本金)、`fee_amount`(手续费)、`payable_amount`(本次应付) numeric(12,2)
- 图片以 BYTEA 存储（复用 QR 范式）：`image_data bytea`、`mime_type`、`byte_size`、`sha256 char(64)`、`original_filename_safe`（随机化后的安全名，不落磁盘路径）
- `status`（见状态机）、`submitted_at`、`reviewed_by_admin_id`、`reviewed_at`、`reject_reason`
- `linked_payment_id uuid null`（核对通过时关联/创建的正式 `payments` 行）
- 索引：`(user_id, submitted_at desc)`、`(status, submitted_at desc)`、`(cn_code)`
- 迁移可重复安全执行（`create table if not exists` + `add column if not exists`），并补迁移测试。

### 设计：状态机（拟定，未实施）
```
未提交(no row)
  └─用户选图并提交、后端校验+落库成功─▶ submitted「已交肾（待管理员核对）」
        ├─管理员通过─▶ approved「核对通过」 （此时且仅此时创建/批准正式 payments 行 → 计入已付）
        └─管理员驳回─▶ rejected「已驳回」（带 reject_reason）
                          └─用户重新提交─▶ submitted（新一轮）
voided（与凭证区分）：撤销的是"正式 payments"，不是凭证提交；已撤销付款不因存在旧凭证而复活。
```
- 上传中/失败：`submitted` 只有在图片校验通过且事务落库成功后才写入；上传失败、MIME 非法、超限、后端保存失败一律**不产生 submitted**，用户状态不变。
- **绝不把 `payments.status` 直接改成 approved**；也**绝不因为"图片存在"就加已付**。approved 只经"管理员核对"这一条路径。

### ⛔ 需要你拍板的一个财务语义（不猜测，故在此停止）
`payments` 只有经 `payment_items.applied_amount` 分配到具体订单明细、且 `status='approved'` 才计入已付。凭证提交捕获的是"本次应付总额 + 付款方式"，但**明细级分配是业务决定**。管理员"核对通过"时，正式付款如何分配到订单明细，有两种最小安全方案，需你选择：

- **方案 A（推荐，改动最小、最安全）**：管理员点"通过"时，**带着该 CN 的凭证信息跳转/预填现有"录入付款"流程**（`CreatePayment`，已支持按明细分配、手续费、幂等），管理员确认分配后才真正 approved。凭证 `linked_payment_id` 记录生成的付款。优点：完全复用已验证的付款分配与撤销逻辑，凭证只做"证据 + 待办"，零财务口径风险。
- **方案 B**：凭证在提交时就携带一份"建议分配"（按未付明细自动铺满到 payable 金额），管理员一键通过即按该分配创建 approved 付款。优点：管理员一步到位；缺点：需要在提交侧就做明细分配与并发/撤销一致性处理，财务口径面更大。

在你确认方案（A 或 B）前，我不会写核对通过→创建正式付款这段财务代码，以免猜测明细分配语义。其余部分（表、状态机、上传安全、用户提交 UI、管理员列表/详情、驳回重提、鉴权图片服务）在方案确定后即可按上述设计实施。

### 本阶段停止点（符合指令）
指令 11A 要求"先输出并记录数据模型与状态机，再实施"，且阶段 11 结尾要求"停在人工验收点"。故在此**停止**，等待你对上面方案 A/B 的选择，再进入 11B～11H 的实施与安全测试。

## 阶段 11D（部分）：迁移 0021 与安全校验准备（2026-07-18）

你已确认 **方案 A**（管理员核对通过时复用现有 `CreatePayment` 录入付款流程分配到明细，凭证只做证据+待办）。据此落地了本阶段的安全基础，并在此设定清晰的实施续接点。

### 已完成并验证
- **迁移 `0021_payment_submissions.sql`**（新表，独立于 `payments`）：
  - 图片以 `bytea` 存储（复用 `payment_qr_codes` 范式，无磁盘路径、无用户文件名落盘 → 天然规避目录穿越，随 pg_dump 备份）。
  - `status ∈ (submitted, approved, rejected)`；`byte_size ≤ 10485760`(10MiB)；`sha256 char(64)`；金额三列 `numeric(12,2)`。
  - 约束：rejected 必须有非空 `reject_reason`；approved 必须有 `linked_payment_id`（凭证核对通过必然关联一条正式付款）。索引：`(user_id,submitted_at)`、`(status,submitted_at)`、`(cn_code)`、`(linked_payment_id)`。可重复安全执行。
  - **隔离验证**：重启 8090（新 PID 22284）→ 启动日志 `database migration applied: 0021_payment_submissions.sql`；`current_database=pjsk_qr_dev`、表 20 列、`schema_migrations` 已记录、0 行。**仅对 pjsk_qr_dev 应用**，正式 `pjsk` 未触碰。
- **安全校验复用**：为 `internal/paymentqr` 新增 `ValidateImageWithLimit(data, maxBytes)`，`ValidateImage` 委托之。凭证上传（≤10MiB）由此复用**已验证**的魔数嗅探 + 结构解码(PNG/JPEG/WebP) + 25MP 解压炸弹上限 + SHA256，不重写安全关键校验。既有 QR 测试全绿。

### 状态确认
- 全量回归通过：后端 `go build`/`go test ./...` **18 包全 PASS**；前端 `vue-tsc -b` + **171/171**；`git diff --check` 干净；HEAD 仍 `c8e7df0`，未提交/未推送/未使用子代理/未改真实业务数据。

### 续接点（11B/11C/11E–11H 的实施计划，按方案 A）
1. 新包 `internal/paymentsubmission`：types/store/filters/facets/handler。
2. 用户接口：`POST /api/query/payment-submissions`（校验图片→事务落库→返回 submitted）、`GET /api/query/payment-submissions`（本人）、`GET /api/query/payment-submissions/{id}/image`（仅本人、nosniff、无绝对路径）。上传失败/非法/超限/落库失败一律不产生 submitted。
3. 管理员接口：`GET /api/admin/payment-submissions`（WPS 表头筛选：CN/付款方式/提交状态/本金/手续费/本次应付/提交时间/核对时间/核对管理员）+ facets + 分页；`GET /{id}`、`GET /{id}/image`（需管理员会话）；`POST /{id}/reject`（原因必填）；`POST /{id}/approve`（**复用 `payments.CreatePayment` 明细分配创建 approved 付款**，同一操作内标记凭证 approved+`linked_payment_id`；以凭证 id 作幂等键防重复）。
4. 财务不变量测试：仅提交不增加已付；approve 后才增加；驳回可重提；voided 与凭证区分。安全测试：未选图不提交、错误 MIME/超大/伪装文件拒绝、随机化文件名、防穿越、本人/管理员鉴权隔离、partial 清理、日志不含图片内容/查询码/敏感路径。
5. 前端：用户付款中心"提交收肾记录"弹层（选图→预览→本金/手续费/本次应付→提交→"已交肾（待管理员核对）"/驳回原因/重提）；管理员"收肾记录" WPS 列表 + 详情（图片居中、通过/驳回、原因必填、技术标识收起）。

**在此停止**：阶段 11 是指令规定的人工验收停止点，且为财务+安全关键代码。基础（迁移、安全校验、方案 A 决策）已落地并隔离验证；上述 2～5 为下一续接批次，完成并全部业务/安全测试通过后再进入阶段 12/13。5173(PID 28336)/8090(PID 22284) 隔离服务保持运行，均指向 `pjsk_qr_dev`。

## 阶段 11B 接手：只读接管检查（2026-07-18，Codex 续接）

续接方案 A，从阶段 11B 开始实现「收肾记录付款凭证」。开始前只读确认：

### 基线
- 分支 `main`；`HEAD` = `origin/main` = `c8e7df0d7143137b8f6374d2b376a42bb8530582`，与交接一致。
- `git status`：多轮未提交成果完整保留（HANDOVER、门户/筛选/二维码后端与前端、迁移 0020/0021、各测试、日志），另有工作树根目录 3 个导出 XLSX（`order-items-*.xlsx`/`payments-*.xlsx`/`users-*.xlsx`，**本轮不纳入 Git**）。
- 未使用：git reset/clean/checkout 覆盖/restore 覆盖/强推；未删除或移动历史日志。

### 已就绪的 11 基础（复核）
- 迁移 `0021_payment_submissions.sql`（独立于 payments；bytea 图片；status∈submitted/approved/rejected；byte_size≤10MiB；金额三列 numeric(12,2)；约束 rejected 必有 reject_reason、approved 必有 linked_payment_id；索引齐全；可重复执行）。
- `paymentqr.ValidateImageWithLimit(data,maxBytes)` 已抽取（魔数嗅探 + PNG/JPEG/WebP 结构解码 + 25MP 解压炸弹上限 + SHA256），凭证上传直接复用，不重写安全校验。
- `internal/paymentsubmission` 包尚不存在——本轮从零新增。

### 复用范式（只读确认，不改语义）
- 上传/图片服务范式：`paymentqr`（bytea 存储、`http.MaxBytesReader`、nosniff/ETag/private 缓存、日志仅元数据）。
- 付款事务核心：`payments.PostgresStore.CreatePayment`（幂等键 advisory lock + 明细行锁 + 超额校验 + 整数分手续费 + payment/payment_items 插入 + `recalculateUserPaymentStatus`）。方案 A 需在**同一事务**内复用它，故先抽取 `CreatePaymentTx(ctx, tx, ...)` 共享事务核心，`CreatePayment` 变为其薄封装，并补回归测试。
- 筛选/facets 范式：`payments/filters.go` + `facets.go`（`baseCTE` 单一事实源、`argList` 全参数化、`buildConditions(skipColumn)`、多值 `= any($n::text[])`、范围/日期、`<column>_blank=1`、分页 total、facets `count(*)` 行数口径、精确路由先于前缀）。
- 用户会话：`query.Handler.RequireSession` 现不注入身份；将新增 `RequireSessionUser` 中间件把可信 `user_id/cn_code`（来自会话，非 multipart）注入 context，供凭证用户接口取用，不改既有 `RequireSession`。

### 本轮不做
- 不提交、不推送、不执行正式 `pjsk` 迁移、不上传真实凭证/二维码、不使用子代理、不访问正式 8080/`pjsk`。

## 阶段 11B/11C/11E–11G 后端实现（2026-07-18，完成，方案 A）

### 新增/修改文件
- 新增包 `backend/internal/paymentsubmission/`：`types.go`、`filters.go`、`facets.go`、`store.go`、`handler.go` + 测试 `handler_test.go`、`filters_sql_test.go`、`facets_test.go`、`privacy_boundary_test.go`、`submission_integration_test.go`。
- 改 `backend/internal/payments/handler.go`：抽取共享事务核心 `CreatePaymentTx(ctx, tx, req, adminID)`，`CreatePayment` 变为其 Begin/Commit 薄封装；新增导出 `FeeForPrincipalCents`（复用既有 `calculateFee`，不另写手续费算法）与 `OutstandingPrincipalCents`（复用 `listItemsForUserTx` 汇总，读用户未付本金）。
- 新增 `backend/internal/payments/create_payment_tx_integration_test.go`：对既有付款录入流程补回归测试（提交/回滚原子性、手续费口径）。
- 改 `backend/internal/query/handler.go`：新增 `RequireSessionUser` 中间件与 `SessionUser`/`CurrentSessionUser`/`ContextWithSessionUser`，把**会话可信**的 `user_id/cn_code` 注入 context；既有 `RequireSession` 不变。
- 改 `backend/internal/api/router.go`：注册用户/管理员凭证路由（facets 精确路径先于 `/{id}` 前缀）。

### 11B/11C 用户接口
- `POST /api/query/payment-submissions`：multipart（`file` + `payment_method`）。CN、user_id **只来自会话**，忽略 multipart 里任何 cn/user_id/金额字段（专项测试 `TestUserCreateSucceedsAndTakesIdentityFromSessionNotBody`）。图片经 `paymentqr.ValidateImageWithLimit(≤10MiB)`（魔数嗅探 + PNG/JPEG/WebP 结构解码 + 25MP 上限 + SHA256）后才落库；本金取 `OutstandingPrincipalCents`、手续费取 `FeeForPrincipalCents`（支付宝 0、微信整数分向上取整），三额存 `numeric(12,2)`。校验失败/无图/非法 MIME/超限一律不产生 submitted。`original_filename_safe` 为清理后的安全展示名（去路径分隔符、白名单字符、空则回退「收肾记录.<ext>」），从不落磁盘、从不拼路径。
- `GET /api/query/payment-submissions`：仅本人记录，DTO 不含 image/SHA/user_id/linked_payment_id/管理员 ID（`privacy_boundary_test` 真序列化断言）。
- `GET /api/query/payment-submissions/{id}/image`：按 `user_id` 限定本人；他人/不存在一律 404（`TestUserCannotReadForeignImage`）；响应 `Content-Type` + `X-Content-Type-Options: nosniff` + `private` 缓存 + ETag(SHA 只在 ETag 头)。

### 11E 管理员接口 + WPS
- `GET /api/admin/payment-submissions`（后端分页 total/total_pages/page/page_size，默认 50、上限 200）；`/facets`；`/{id}`（详情，业务字段 + 技术标识区字段）；`/{id}/image`（nosniff）；`/{id}/reject`；`/{id}/approve`。
- WPS 筛选与订单/用户/付款同构：`baseCTE` 单一事实源（**从不 select image_data**，测试锁定）、`argList` 全参数化、`buildConditions(skipColumn)`、值列 CN/付款方式/提交状态/核对管理员多值 `= any($n::text[])`、范围本金/手续费/本次应付、日期提交时间/核对时间（`reviewed_blank=1` 未核对）、count 与 list 逐字同 WHERE、facets `count(*)` 按凭证行数、中文标签（待核对/已通过/已驳回、支付宝/微信复用 `payments.MethodLabel`）、列名白名单、搜索 `%/_/\` 转义。
- 驳回：仅 submitted 可驳回；原因去空白后必填；已核对（approved/rejected）返回 409；记录 reviewed_by/reviewed_at/reject_reason。

### 11F 审核通过 = 复用付款事务核心（关键）
- `store.Approve`：先 `select ... for update` 锁凭证行并校验仍为 submitted；用凭证的 **cn_code（可信）** 与管理员显式明细分配构造 `CreatePaymentRequest`，幂等键 = `"payment-submission:"+id`（服务端派生），在**同一事务**内调用 `payments.CreatePaymentTx`；付款创建成功后同事务把凭证置 approved + `linked_payment_id` + reviewed_by/at（`RowsAffected==1` 校验），再 Commit。付款创建与凭证 approved 标记同生共死。
- 双重并发保护：凭证行锁（主）+ 幂等键（备）。不复制金额/手续费/明细分配/状态更新算法。

### 11G 财务不变量 + 安全（自动测试全绿）
- 后端 `go fmt`/`go vet`/`go build` 干净；`go test ./...` **19 个包全部 PASS**（新增 paymentsubmission 包 41 单元测试）。
- 数据库集成测试（`PJSK_RUN_DB_INTEGRATION_TESTS=1`，跑在**一次性隔离库 `pjsk_integration_test_*`**，harness 硬禁 `pjsk`，用后即删——未触碰 `pjsk` 也未触碰 `pjsk_qr_dev`）：
  - 仅提交：`payment_submissions +1`、`payments 0`、已付 0 不变（`TestSubmitOnlyDoesNotChangePaidAmount`）。
  - 审核通过：恰好 1 条 approved payment、`payment_items.applied_amount` 正确、`linked_payment_id` 正确、已付按正式付款更新（`TestApproveCreatesOnePaymentAndLinks`）。
  - 驳回不影响已付；驳回后可重提新记录、旧记录保留（`TestRejectKeepsPaidAndAllowsResubmit`）。
  - 重复审核不重复创建付款（`TestDuplicateApproveDoesNotCreateSecondPayment`）。
  - 8 并发审核只有 1 成功、恰好 1 条付款（`TestConcurrentApproveOnlyOneSucceeds`）。
  - 正式付款 void 后已付回退，旧凭证保持 approved+linked 历史、不复活（`TestVoidAfterApproveRevertsPaidButKeepsProofHistory`）。
  - 已驳回不能再通过、超额分配被拒且无残留付款、凭证仍 submitted（`TestApproveRejectedSubmissionRefused`/`TestApproveOverPaymentRejectedWithoutResidualRows`）。
  - 用户 A 读不到用户 B 的记录/图片（404）。
  - `CreatePaymentTx` 回归：调用方提交则落库、回滚则无残留、微信手续费 100.01→0.11（`create_payment_tx_integration_test.go`）。
- 安全（单元）：未选图/非法 MIME/伪装 HTML/无图不产生 submitted；未登录用户接口 401、未登录管理员接口 401、驳回/通过要求管理员会话；`safeDisplayName` 防目录穿越（`../../etc/passwd`→`passwd`、`C:\..\proof.png`→`proof.png`）；主表/用户 DTO 无 SHA/内部 ID；错误响应不外泄 SQL/表名/路径（`writeFilterError`/`writeReviewError` 固定文案 + `logsafe.Category`）；日志仅元数据。

### 状态
- 前端（11H）尚未实现，接口/状态机已就位。未提交、未推送、未执行正式迁移、未修改真实业务数据、未使用子代理、未访问正式 8080/`pjsk`。

## 阶段 11H 前端实现（2026-07-18，完成，待隔离浏览器验收）

### 修改/新增文件
- 改 `frontend/src/api/client.ts`：新增收肾记录 API 类型与函数（用户 `listUserPaymentSubmissions`/`submitPaymentSubmission`；管理员 `listAdminPaymentSubmissions`/`getAdminPaymentSubmissionFacets`/`getAdminPaymentSubmissionDetail`/`rejectPaymentSubmission`/`approvePaymentSubmission`）。用户 DTO `UserPaymentSubmission` 不含 sha/image/user_id/linked_payment_id/管理员。
- 改 `frontend/src/App.vue`：新增路由 `admin-submissions`/`admin-submission-detail`（`/admin/finance/submissions[/{id}]`）；收付款子导航与门户卡片加「收肾记录」；管理员 WPS 列表、详情（图片居中 + 驳回 + 通过复用录入付款分配 + 技术标识区）；普通用户付款中心「提交收肾记录」区（选图/本地预览/方式·本金·手续费·本次应付/提交/历史/驳回原因重提）。
- 改 `frontend/src/style.css`：`.submission-records-table`（宽表仅容器内滚）、提交区两列响应式（≤640px 单列）、预览/详情图片固定居中槽位 `object-fit: contain`（完整显示不拉伸）、历史列表、驳回原因输入等。
- 改 `frontend/tests/technical-identifiers.test.mjs`：技术区计数 5→6（新增收肾记录详情技术区，契约不变：默认收起 + 统一「技术标识」+「仅供技术排查」）。
- 新 `frontend/tests/payment-submissions.test.mjs`（12 项）：API 层、管理员 WPS 复用既有组件与列、结果数/分页/清空、主表无技术字段、详情图片居中/通过·驳回/技术区、用户提交入口/预览/金额/禁用逻辑/历史/驳回原因、320px 单列。

### 关键设计
- **审核通过复用现有明细分配 UI**：管理员详情点「加载该 CN 未付明细」→ 复用录入付款的 `loadCNPayment`/`selectedPaymentItemIds`/`paymentAmounts`/`setPaymentItemSelected`/`paymentAmountInvalid`/`paymentAmountValue` 状态与勾选+分摊输入，确认后 `approvePaymentSubmission(id, {items, note})`；不新写一套分配算法与界面。
- **用户身份/金额不信任前端**：提交只发 `file` + `payment_method`（来自付款中心当前选择）；CN、user_id、本金/手续费/本次应付全部后端从会话与既有规则派生。
- **WPS 复用**：管理员列表用既有 `ColumnValueFilter`/`ColumnRangeFilter`/`ColumnDateFilter`；未重写筛选组件。列：CN、付款方式、本金、手续费、本次应付、提交状态、提交时间、核对时间（含「未核对」空白）、核对管理员、详情。
- **技术字段隐藏**：用户页与管理员主表无 SHA/内部 ID/管理员 ID/路径；技术标识（凭证 ID/SHA/格式大小/关联付款 ID）仅在管理员详情底部默认收起区。

### 前端完整测试
- `vue-tsc -b` 通过；`pnpm run build` 通过；`pnpm.cmd test` / `node --test tests/*.test.mjs` → **183/183 通过**（原 171 + 新 12；既有测试未删减，仅技术区计数按新增合规详情从 5 改 6）。

### 状态
- 隔离浏览器验收（8090 重建重启 + pg_stat_activity 反查 + 用户提交/管理员核对全链路 + 320px + Console）待下一步执行。未提交、未推送、未执行正式迁移、未修改真实业务数据、未上传真实凭证、未使用子代理、未访问正式 8080/`pjsk`。

## 阶段 11 隔离验收（2026-07-18，完成，停在人工验收点）

### 隔离环境实证
- 重建后端到 `C:\tmp\pjsk-submission-dev.exe`（当前工作树，含 paymentsubmission 全部代码），停掉旧隔离 PID 22284（`C:\tmp\pjsk-order-filters-dev.exe`）后以 `DATABASE_URL=<LOCAL_ISOLATED_DATABASE_URL>`、`APP_PORT=8090`、`SERVER_HOST=127.0.0.1` 隐藏启动，新 **8090 PID = 41196**，`/health` 返回 `{"status":"ok","database":"connected"}`。
- **PostgreSQL 反查（非仅凭启动参数）**：8090(PID 41196) 到 5432 的连接本地端口 55813；`pg_stat_activity` 反查该 `client_port` 的 **`datname = pjsk_qr_dev`**。
- 前端 5173 重启（新 PID 4196）并显式 `VITE_BACKEND_TARGET=http://127.0.0.1:8090`（`vite.config.ts` 默认 8080，故此覆盖必需）；实测 `GET http://127.0.0.1:5173/api/config` 经代理命中 8090，链路 浏览器→5173→8090→`pjsk_qr_dev`。
- **未访问、未停止、未修改正式 8080（PID 21248）/ 正式库 `pjsk`**；迁移 0021 仅存在于 `pjsk_qr_dev`。
- 测试图片全部程序生成（Go image/png 生成的小图 + canvas.toBlob；另用 25 字节 HTML 伪装 `.png` 做拒绝测试）；**未上传任何真实付款凭证或真实二维码**。隔离测试账户用明确前缀 `PAYSUB_UAT`（admin `paysub_uat_admin`、用户 `PAYSUB_UAT_CN` 60.00 未付），验收后按前缀精确清理，`测试CN01`（1 用户 / 2 付款）完整保留。

### HTTP 端到端验收（curl 直连隔离 8090，真实会话）
- 用户登录 200；仅提交凭证：提交前已付 0.00/未付 60.00 → 微信提交（本金 60、手续费 0.06=ceil(6000/1000)、应付 60.06）成功返回 submitted → **提交后已付仍 0.00/未付仍 60.00**（财务不变量：仅提交不动已付）。
- 用户列表 DTO 键仅 `id/payment_method/principal_amount/fee_amount/payable_amount/status/submitted_at`——**无 sha/image_data/user_id/linked_payment_id/reviewed_by**。
- 图片鉴权：本人图片 200 且带 `Content-Type: image/png` + `X-Content-Type-Options: nosniff` + `Cache-Control: private, must-revalidate` + `ETag`；未登录取图 401；随机/非本人 UUID 404 无泄漏。
- 鉴权隔离：管理员接口未登录 401、带**普通用户 Cookie** 亦 401；管理员登录 200。
- 驳回：空原因 400（`驳回原因不能为空`）；带原因 → status=rejected + reviewed_by；重复驳回 409；用户驳回后重新提交 → **新一行 201，旧 rejected 记录保留**（用户历史 3 行：新 submitted + 旧 rejected + 另一条）。UTF-8 原因经文件回传，DB `octet_length=27/char_length=9`，API 原样返回「图片不清晰，请重拍」（早前命令行 Chinese 乱码仅为 Windows 控制台代码页假象，非后端问题）。
- 审核通过（复用录入付款分配）：管理员取 `/api/admin/payments/unpaid?cn=` 未付明细 → 以 `{order_item_id, amount:60}` 分配 POST approve → **status=approved + linked_payment_id + reviewed_by；用户已付变 60.00/未付 0.00；`payments` 恰好 1 条、payable=60.06（本金 60 + 微信手续费 0.06）**。
- 重复点击通过 → 409，`payments` 仍 1 条（幂等，不重复付款）；对已 rejected 凭证再通过 → 409。

### 浏览器可视验收（受控预览浏览器，5173→8090，320×720）
- 用户 `/query/payment`：无页面级横滚（docW=clientW=320）；「提交收肾记录」入口存在，**未选图时提交按钮 disabled**；显示 本金/手续费/本次应付；「我的收肾记录」历史 3 条，驳回项显示中文原因「图片不清晰，请重拍。可重新选择图片后再次提交。」；页面无 64 位十六进制 SHA。
- 管理员 `/admin/finance/submissions`：无页面级横滚；标题「收肾记录」、`结果：共 N 条收肾记录`、`清空全部筛选`；**9 个 WPS 表头漏斗**（CN/付款方式/本金/手续费/本次应付/提交状态/提交时间/核对时间/核对管理员，复用既有 Column* 组件）；宽表仅容器内部滚动；主表无 SHA。
- 管理员详情：图片经鉴权端点加载成功、`justify-content: center` 居中、`object-fit: contain` 不拉伸；**驳回原因空时驳回按钮 disabled**；含「加载该 CN 未付明细 + 勾选 + 分摊 + 确认通过并创建付款」分配流程；技术标识 `<details>` **默认收起**，DOM 含「仅供技术排查，日常对账与查询无需使用。」，技术项仅 凭证 ID/SHA-256/图片格式·大小；主汇总区不含 SHA。
- **Console 全程无红色错误**（用户与管理员各页 `onlyErrors` 均空）。

### 结论
- 阶段 11 全部业务/安全/财务自动测试与可在隔离环境完成的 HTTP + 浏览器验收均通过。5173(PID 4196)/8090(PID 41196) 隔离服务保持运行，均指向 `pjsk_qr_dev`，供人工复核。
- **未提交、未推送、未执行正式迁移、未上传真实凭证、未修改真实业务数据（测试CN01 完整保留）、未访问正式 8080/`pjsk`、未使用子代理。**

## 阶段 11 补丁：未付明细一键全选（2026-07-18，完成，停止点）

人工功能验收基本通过后按指令做最小功能补丁，不做大范围视觉重构。

### 修改文件
- `frontend/src/App.vue`：管理员收肾记录详情「核对通过并创建付款」区新增「全选待付明细 / 取消全选」按钮与「已选 N / M 条」计数。
- `frontend/tests/payment-submissions.test.mjs`：新增 2 项专项测试（合计 14 项）。

### 实现要点（不引入新财务算法）
- 新增 `selectableCnPaymentItems`（仅 `remaining_amount > 0`，与复选框禁用条件一致）、`allCnPaymentItemsSelected`、`selectAllCnPaymentItems`、`clearAllCnPaymentItemSelection`。
- **两个方向都只逐行调用既有 `setPaymentItemSelected(item, true/false)`**——与管理员逐条手动勾选完全同一入口；默认分摊金额由该函数统一填充（剩余应付），全选函数内无任何金额写入或计算（测试断言禁止 `paymentAmounts.value =`/`formatMoney`/`round` 等出现在函数体内）。
- 不改动 `paymentAmountInvalid` 等既有超额校验；未付明细为空或已全选时「全选」禁用，无选中时「取消全选」禁用。

### 专项测试
- 契约测试锁定：按钮存在与禁用条件、计数即时反映、逐行复用 `setPaymentItemSelected`、可选集合过滤条件、全选函数体不含金额计算、既有超额校验入口未动。`node --test tests/payment-submissions.test.mjs` 14/14 通过。

### 完整验证
- `vue-tsc -b` 通过；`pnpm.cmd run build` 通过；`pnpm.cmd test` / `node --test tests/*.test.mjs` **185/185 通过**；后端 `go test ./...` 全 PASS；`git diff --check` 干净；HEAD 仍 `c8e7df0` = origin/main。

### 隔离环境再实证
- 8090 仍为 PID 41196（`C:\tmp\pjsk-submission-dev.exe`）；`pg_stat_activity` 反查其全部 4 条 5432 连接 **`datname = pjsk_qr_dev`**。5173 仍为 PID 4196（显式 `VITE_BACKEND_TARGET=127.0.0.1:8090`，Vite HMR 使补丁即时生效）。正式 8080/`pjsk` 未访问。

### 浏览器验收（受控预览浏览器，`PAYSUB_UAT` 前缀数据：2 条未付明细 60/40）
- 加载明细前按钮不出现（区域在 `v-if="cnPayment"` 内）；加载后初始「已选 0 / 2 条」、全选可用、取消全选禁用、金额输入禁用。
- 点全选 → 2/2 勾选、金额自动填默认 **60.00 / 40.00（与手工逐条勾选完全一致）**、确认按钮启用、全选变禁用、取消全选启用。
- 点取消全选 → 回到 0/2、确认按钮禁用。
- 部分选中（手工勾第 1 行）后点全选 → 一键补全为 2/2。
- 超额校验未被绕过：把分摊改为 999（剩余 60）→ 输入框立即出现 `invalid` 态；改回 60.00 恢复。
- 页面无横向滚动；**Console 无红色错误**。
- 验收后按 `PAYSUB_UAT` 前缀精确清理（0 用户/0 管理员残留）。库中剩余 3 条凭证与 `测试CN01` 的第 3 条付款为**用户人工验收自建的测试数据**（如凭证 `64c71e94-…`），按"只清理本轮自建数据、不宽泛删除"规则原样保留。

### 登记为后续 Claude 前端视觉整改项（本轮不改）
1. **驳回原因输入框过窄**：改为全宽多行 `textarea`，最小高度约 96px。
2. **「返回收肾记录」等返回按钮不够突出**：统一为清晰的次要操作按钮（明确边框/背景/悬停态，与页面背景明显区分），位置与样式全站统一，不只改收肾记录页。

### 状态
- 已停止。未提交、未推送、未执行正式迁移、未访问正式 8080/`pjsk`、未进入网络部署、未使用子代理。5173(4196)/8090(41196) 保持运行供复核。下一批次：阶段 13 完整测试 + 敏感信息扫描 + Git 范围核对，输出待提交清单后等待批准。

## 阶段 13 收尾检查（2026-07-18，进行中）

### 接管与只读基线
- 已阅读 `AGENTS.md`、`HANDOVER.md`、本日志，以及 payment submissions、一键全选、付款分配和前端专项测试相关当前文件；以当前工作树为唯一基线，不整理、不覆盖既有成果。
- 分支为 `main`；`HEAD` 与 `origin/main` 均为 `c8e7df0d7143137b8f6374d2b376a42bb8530582`，与交接记录一致。
- 接管时 `git diff --check` 无输出；工作树包含既有修改、新增文件及根目录 3 个 XLSX 导出产物，均保持原样，未暂存、未提交、未删除。
- 已确认一键全选/取消全选仍逐行复用 `setPaymentItemSelected`，计数与 `paymentAmountInvalid` 既有校验入口仍在；本阶段不改两项已登记视觉问题。
- 本轮未使用子代理，未访问正式 8080/`pjsk`。

### 后端完整验证
- `go fmt ./...` 通过且无文件输出（未格式化改写既有 Go 文件）。
- `go test ./...` 全部通过（数据库集成测试保持默认关闭，未触碰任何数据库）；`go vet ./...` 与 `go build ./...` 均通过。
- 未发现可稳定复现的生产代码错误，未修改 Claude 已完成的生产业务代码。

### 前端完整验证
- `node_modules/.bin/vue-tsc.CMD -b`、`pnpm.cmd run build` 均通过。
- `pnpm.cmd test` 与 `node --test tests/*.test.mjs` 均为 **185/185 通过**，0 失败、0 跳过。
- 一键全选专项 `node --test tests/payment-submissions.test.mjs` 为 **14/14 通过**。

### 隔离数据库集成测试
- 显式设置 `PJSK_RUN_DB_INTEGRATION_TESTS=1`，并从测试进程移除 `DATABASE_URL`；执行 `go test ./... -count=1 -v` 全部通过。
- 测试工具继续执行硬阻断：不加载 `.env`、不读取 `DATABASE_URL`，维护连接仅允许本机 `postgres`，数据库名仅允许 `pjsk_integration_test_*`，正式库 `pjsk` 明确拒绝。
- 每个一次性测试库均由工具精确创建、迁移并在测试清理阶段精确删除；测试后从本机 `postgres.pg_database` 只读反查，`pjsk_integration_test_*` 残留数为 **0**。
- 未访问或写入 `pjsk_qr_dev`，未访问正式 `pjsk`。

### 隔离链路确认
- 5173 监听进程为 PID **4196**（`D:\nodejs\node.exe`，Vite）；8090 监听进程为 PID **41196**，程序路径 **`C:\tmp\pjsk-submission-dev.exe`**。
- `frontend/vite.config.ts` 的 `/api`、`/health` 代理均使用 `VITE_BACKEND_TARGET`；经 5173 请求 `/api/config` 返回 200，并在请求期间从 TCP 实际连接捕获到 **PID 4196 → 127.0.0.1:8090**，明确证明当前 5173 代理命中 8090。
- 从 PID 41196 的实际 PostgreSQL 连接取得本地端口 63233、63235、63237，再通过 `postgres.pg_stat_activity` 只读反查：3 条连接的 `datname` 均为 **`pjsk_qr_dev`**（对应 PostgreSQL backend PID 31708、34800、31864）。
- 未探测、未访问、未停止、未修改正式 8080；未访问正式数据库 `pjsk`。

### 敏感信息扫描与 Git 范围核对
- 拟提交候选共 89 个文本文件（19 个已跟踪修改、70 个新增）；无拟提交二进制文件。未发现密码、私钥、证书私钥、Bearer/Cookie/session/token 值、SMTP 密码、真实查询码或哈希、Base64 图片、真实二维码或真实付款凭证。
- 发现待用户裁决项：既有未跟踪开发日志的历史段落含本机用户目录绝对路径；7 月 17 日日志历史段落还含无密码的本地开发数据库连接串。因规则禁止重写旧段落，本阶段未擅自脱敏或排除日志；在用户明确批准处理方式前，不应提交。
- 当前无删除、移动、重命名或复制状态，无暂存文件；除 3 个根目录 XLSX 导出产物外，未发现其他超出当前阶段 3～11 累积成果范围的文件。
- 明确排除且保持未跟踪：`order-items-20260716-200204.xlsx`、`payments-20260716-200317.xlsx`、`users-20260716-195154.xlsx`。未发现 `.env`、本地图片/截图、临时 SQL/PowerShell/curl/Cookie、临时 exe、运行日志、dump/validation/partial、`node_modules`、构建缓存或 `.claude/settings.local.json` 出现在 Git 状态中。

### 阶段 13 停止点
- 分支、`HEAD`、`origin/main` 保持不变；最终 `git diff --check` 无输出。
- 工作树无暂存内容，未执行 `git add`、提交、推送、正式迁移、部署或服务重启；未修改 Claude 一键全选补丁或其他生产业务代码。
- 阶段 13 验证本身无测试失败；唯一阻塞为开发日志历史段落的敏感路径/本地连接串如何处理，等待用户明确批准。
- 本轮未使用子代理。未提交、未推送、未执行正式迁移、未部署、未上传真实凭证、未访问正式 8080/`pjsk`。

## 提交前隐私脱敏说明（2026-07-18）

本日志在首次纳入 Git 前进行了最小隐私脱敏：仅将包含本机用户名的用户目录改写为 `%USERPROFILE%`、`%LOCALAPPDATA%` 或 `%APPDATA%` 形式，并将完整本地数据库连接串改写为 `<LOCAL_ISOLATED_DATABASE_URL>`。功能结论、测试结果、数据库归属、端口、迁移编号、执行顺序和安全边界均未改变。
