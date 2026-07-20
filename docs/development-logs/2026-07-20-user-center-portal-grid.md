# 用户中心门户四卡布局修复（阶段 2H-2A，2026-07-20）

> 授权范围：仅前端布局与相关测试。未修改后端、迁移、数据库、鉴权、部署目录或云端运行状态；未部署到本地旧生产或云端。

## 问题与根因

SSH 隧道人工验收（阶段 2H-1）发现云端新版用户中心（`/query`）上下错位：

1. 统计区 `class="summary-grid portal-summary"` 落在 `.summary-grid` 的 **5 列默认模板**里，4 个 tile 占前 4 列、右侧空一列，整体偏左且无 max-width 居中。
2. `.module-grid` 用 `repeat(auto-fit, minmax(220px, 1fr))` 且 `max-width: 920px`，四列最小需宽 4×220+3×18=934px > 920px，**数学上永远只能排 3 列**，"账户安全"必然掉行。
3. 两网格 max-width（无 / 920px）与 gap（12px / 18px）不一致，列线无法对齐。

## 修改内容

| 文件 | 修改 |
|---|---|
| `frontend/src/style.css` | 新增"阶段 2H-2A"样式段（35 行）：`.summary-grid.portal-summary` 与 `.module-grid.module-grid--user` 共用规则（四列等宽、gap 16px、max-width 1040px、水平居中）；1199px 断点两列（max-width 720px）、640px 断点单列（max-width 420px）；`.module-grid--user` 内卡片槽位对齐（标题顶、说明弹性居中、CTA 沉底）；`.module-card__meta--empty` 隐形占位；`.summary-grid.query-pay-summary` 修正为 4 列并随断点降列 |
| `frontend/src/App.vue` | 用户中心功能区网格加 `module-grid--user` 类；四张 ModuleCard 显式加 `reserve-meta` |
| `frontend/src/components/ModuleCard.vue` | 新增可选 prop `reserveMeta`；meta 徽标改为 `meta || reserveMeta` 时渲染，空值挂 `module-card__meta--empty` 占位类。未传参时行为与原版完全一致 |
| `frontend/tests/user-portal-grid.test.mjs` | 新增 7 项源码断言测试（见下） |

作用域控制（授权要求 6）：全部新规则使用双类/后代选择器收敛于 `portal-summary`、`module-grid--user`、`query-pay-summary` 三个显式类；通用 `.module-grid`（管理端 3 处使用）、通用 `.summary-grid` 默认 5 列模板及其既有媒体查询原样保留；`module-grid--user` 全站仅用户中心一处。字体、文案、颜色、业务逻辑零改动。

`query-pay-summary`（付款中心"付款汇总"4 tile）确认同属 5 列模板导致的 4/5 偏左问题，按授权要求 8 用同一受限方式修正列数；因其位于面板内，不附加容器 max-width。

## 测试与构建

- 新增 `user-portal-grid.test.mjs` 7 项断言：共用容器规则、两级断点同步降列、通用选择器未被改写（含"新样式段禁止裸选择器"逐行扫描）、模板专用类唯一性与四卡 reserve-meta、ModuleCard 默认行为不变、卡片对齐规则作用域、付款汇总修正。
- `pnpm run test`：**194/194 通过**（既有 187 项零回归）。
- `pnpm run build`（`vue-tsc -b && vite build`）：通过；新产物 `dist/assets/index-BxRj2fUW.js`（284.74 kB）、`index-DIJ7X15Q.css`（52.88 kB）。
- `git diff --check`：通过。改动统计：3 文件 44 insertions / 6 deletions + 1 新测试文件。

## 人工预览验收点（待执行）

1. ≥1200px：统计 4 列与功能 4 列同宽同心，首尾边缘齐平；"账户安全"位于第一行第 4 列。
2. 四卡标题、说明、meta 徽标、CTA 四条水平线对齐（账户安全 meta 行为隐形占位）。
3. 约 1000px：两网格同步 2×2，中心线仍对齐。
4. ≤640px：单列，无横向滚动、无贴边溢出。
5. 付款中心"付款汇总"4 tile 等宽满行，不再偏左。
6. 回归：系统主页分流、谷子管理中心门户、管理端数据/财务模块行、各 metrics 网格布局不变。

## 未完成项与边界

- 本轮构建产物仅存于 `frontend/dist`（gitignore），**未部署**：本地旧生产（8081）与云端 release `98f8fe1e7eb6` 均不含本修复；`frontend/dist` 自本轮起不再与云端 release 逐字节同源（云端有自己的已签收副本，不受影响）。
- 云端新版验收如需看到本修复，须待下一次 release 打包（新维护窗口流程）。
- 未提交、未推送，停在提交前等待人工预览确认。
