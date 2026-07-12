# 开发日志:用户查询增强与管理员数据管理(2026-07-12 晚)

按《PJSK Goods Manager 后续开发总计划》推进,本轮完成计划中的第一、二、三阶段,每个小阶段独立提交并推送 GitHub。

## 提交清单

| 提交 | 内容 |
|---|---|
| `4cc4dc9` docs: complete payment module closeout | 付款开发日志、录入区与详情页"不可直接修改"提示、`.claude/launch.json` |
| `44629fc` feat: enrich user query with paid amounts, history, and login throttling | 用户查询增强 + 登录限流 |
| `fa33982` feat: add admin user management pages | `/admin/users` 列表与详情 |
| `ab06568` feat: add CN merge with preview and audit log | CN 合并(迁移 `0013`) |
| `17d5e4b` feat: add CSV export for users, payments, and order items | CSV 导出 |

## 迁移编号调查结论(只读,未改动)

- 迁移版本键 = 完整文件名,两个 `0005_*.sql` 在 `schema_migrations` 中均已执行(07-11 08:07 / 17:40),互不冲突。
- 决定:历史文件不重命名,新迁移从 `0013` 继续编号(本轮的 `0013_cn_merge.sql` 即按此执行)。

## 普通用户查询端(`/query`)

- 明细级新增 `paid_amount` / `remaining_amount`;订单级和整体新增已付/未付汇总。
- 新增付款历史(仅金额、方式、时间、状态;不暴露管理员用户名、备注、撤销原因),已撤销付款置灰删除线标注。
- 登录限流(`internal/query/ratelimit.go`):同一 IP 每分钟最多 20 次登录尝试;同一 IP+CN 十分钟内失败 5 次封锁 10 分钟,登录成功即清零。实测:第 6 次错误返回 429。
- 手机端沿用既有响应式规则(表格横滚、窄屏堆叠)。

## 管理员数据管理

- **用户管理** `/admin/users`、`/admin/users/:id`:列表含查询码状态、订单数、总金额、已付、剩余;详情含订单汇总、完整付款记录(含撤销审计)、导入来源、CN 合并历史。
- **CN 合并**(高风险,按计划带双重保护):
  - `GET /api/admin/users/merge-preview` 先预览影响(迁移订单数、付款数、目标用户现状);
  - `POST /api/admin/users/merge` 必填原因 + 前端二次确认弹窗;
  - 事务内按稳定顺序锁行,迁移 orders/payments/query_sessions,源用户标记 `merged` 并清空查询码;
  - `cn_merge_logs` 永久记录源/目标 CN、迁移数量、原因、管理员、时间;
  - 已合并用户不能再作为源或目标(禁止循环合并)。
- **CSV 导出**:用户汇总、付款记录(保留当前筛选条件)、订单明细(支持 `payment_status=unpaid` 即未付明细)。UTF-8 BOM,金额两位小数,单次导出上限 50000 行。

## 验证

- `go fmt` / `go build` / `go vet` / `go test ./...` 全部通过(admin、importpreview、payments、query、users)。
- `pnpm run build`(含 vue-tsc 类型检查)通过。
- 迁移 `0013_cn_merge.sql` 已在本地库成功应用(启动日志确认)。
- 新增管理端接口(users、merge、export)未登录访问均返回 401。

## 留给人工验收(需真实浏览器,localhost:5173)

- 付款撤销弹窗(原生 prompt,自动化浏览器不支持)—— 清单见 `2026-07-12-payment-details-void-audit.md`。
- 用户管理页、CN 合并预览/确认、CSV 导出下载(需管理员登录)。
- `/query` 用 CN + 查询码登录后确认已付/未付/付款历史显示。

## 下一阶段(按计划顺序)

1. Excel 高级处理:差异导入、已付款明细保护、导入纠错历史、角色库/角色别名、分类库及维护页面。
2. 系统管理:管理员权限分级、统一审计日志、后台首页统计、登录安全、自动测试补全。
3. 正式部署(服务器、生产库、Nginx、域名、HTTPS)。
