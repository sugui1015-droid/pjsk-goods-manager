# 2026-07-21 阶段 2H-Final:正式维护窗口、最终数据迁移、生产切换与上线验收

状态:**草稿,待人工审阅后提交**。全程按门禁串行执行,每阶段先证据后推进;无一项关键校验失败,未触发回滚。本文不含任何密码、密钥、恢复码、Cookie、Token 或完整敏感配置。

## 结果摘要

- 正式站点 `https://pjskgoods.cloud` 上线(www 308 → 主域;Let's Encrypt 证书至 2026-10-18;HSTS 暂未启用)。
- 云端 release `95036a07911bfcdbfc62e6278982dd6e268d8447`,数据库 22/0022,owner 恰好 1(admin)。
- **云端正式写入分界点:2026-07-21 00:19:22 CST**(受控测试导入确认)。
- **回滚窗口已由 owner 于阶段十二验收通过后正式关闭**:云端 PostgreSQL 18 为唯一事实源,本地旧生产永久退役为归档,不得再恢复为生产。
- 全部回滚材料保留,未删除任何备份、旧 release、dump 或配置。
- 尚未开放真实客户使用。

## 关键时间线(CST,2026-07-20 → 07-21)

| 时间 | 事件 |
|---|---|
| 07-20 23:16:51 | 维护窗口开始(15:16:51 UTC)。基线复核:HEAD=origin/main=`95036a0791…8447`,工作树干净;本地 pjsk-caddy(8081)/pjsk-backend(8080)/PG18 运行,库 19/0019 |
| 23:2x | 管理员 PowerShell 停止 pjsk-caddy、pjsk-backend;8080/8081 无监听;PG18 保留运行 |
| 23:23:29 | **本地冻结**:pjsk 无业务连接,prepared=0,仍 19/0019。冻结基线:users 45、orders 44(6318.44)、order_items 120(6318.44/qty163)、payments 7(submitted 1145.40/fee 0.44/payable 1145.84)、payment_items 19(1145.40)、products 69、admins 2 |
| 23:24:34 | 生成唯一新 final dump:`D:\PJSK-Archive\migration\pjsk-final-95036a07911b-20260720-152434.dump`,121,895 B,SHA-256 `8CB3D8B5984A1C47908D2C9C10F929C2F1F0AEA236AD4FC9530EC5309909DD7F`,TOC 174;导出前后基线一致;与早前 historical dump(98f8fe1e7eb6/083813,SHA 52DEA912…)明确区分 |
| 23:25 | 上传至云端新建 0700 中转目录 `/home/ubuntu/pjsk-transfer-20260720-final/`;云端 SHA 逐字一致,PG18 `pg_restore --list` TOC 174 |
| 23:26–27 | 云端 pjsk-backend 停止(无 8080 监听、无连接、prepared=0);旧 21/0021 过期库取证备份 `pjsk-stale-pre-rebuild-21-0021-20260720-152737.dump`(131,650 B,SHA `2703faf9…8fd877`,TOC 191,仅留档);记录旧库行数(与本地一致的过期副本) |
| 23:27–28 | drop 旧库;template0 重建(UTF8/en_US.UTF-8/owner=pjsk_app);`pg_restore --no-owner --role=pjsk_app --exit-on-error -1` 恢复成功;核验:19/0019、无 0020+、22 表行数与金额与冻结基线逐分一致、admin UUID `08aca962-9c62-4ec6-a5b1-8684ba612343` active、对象 owner 全为 pjsk_app |
| 23:32 | 服务器本地生成 `ADMIN_RECOVERY_CODE_HMAC_KEY`(CSPRNG,解码 48 字节,与 QUERY_CODE_RECOVERY_HMAC_KEY 哈希比对不同);`/etc/pjsk/backend.env` 原子更新 15→16 变量,root:pjsk 0640,备份 `backend.env.bak-2hfinal-20260720`;密钥值零回显零记录 |
| 23:32:41 | 用新二进制 `promote-owner`(ssh -tt)做配置校验:env/config/连库全通过,按预期在 0022 迁移门禁处拒绝,未启动服务 |
| 23:33 | current 原子切换 → `/opt/pjsk/releases/95036a07911b`(REVISION 完整 SHA、MANIFEST 8/8 OK、x86-64 ELF);旧 release 14d339e56677、98f8fe1e7eb6 全保留 |
| 23:34:07 | 启动 pjsk-backend;迁移 0020→0021→0022 按序各恰好一次;终态 22/0022;新表存在(payment_qr_codes 0、payment_submissions 0、admin_recovery_codes);admin active/testadmin disabled;owner=0;active、NRestarts=0、/health 200、无 error/fatal;历史金额与冻结基线一致 |
| 23:37:27 | **人工 SSH TTY 执行 promote-owner --username admin**:核对 UUID/状态后逐字确认;owner=1,admin role=owner,`owner_promoted` 审计恰 1 条;重复执行被明确拒绝且零副作用 |
| 23:44–45 | Caddy:备份 `/root/caddy-backup-2hfinal-20260720T154401Z/`(原文件 SHA `1d8dd0b4…e953cc6`+adapt 快照);新配置经 fmt/validate/adapt 通过后 reload;23:45:25 两域名证书签发成功 |
| 23:46–49 | 公网验收:https 200、http/www 单跳 308、证书 SNI 正确、/health 200、SPA 子路由 200、IP HTTP 与伪造 Host 均 404(空响应体)、IP HTTPS TLS 握手致命 alert 失败(curl 35/schannel SEC_E_ILLEGAL_MESSAGE)、8080/5432/5433 公网 closed/filtered;首轮发现重定向/404 块泄露 `Server: Caddy`,补 `header -Server` 后复测通过(最终 Caddyfile SHA `48c1f460…04965a0`) |
| 07-21 00:0x | 阶段十一人工功能验收:主页/用户端/管理端/owner 页面/筛选分页导出/双端布局通过,"通过,保留实际运行观察项";恢复码生成项当时未执行(见待办) |
| 00:15–00:19 | 阶段十二检查点 A:管理端正式导入创建测试订单(测试 CN `production_write_test_20260721`,0.10,1 条目);另有 2 条 preview-only 批次(无业务影响);**00:19:22 导入确认 = 云端正式写入分界点** |
| 00:30:14 | 检查点 B:测试 CN 用户端付款中心提交(alipay,principal 0.10/fee 0.00/payable 0.10,jpeg 54,056 B) |
| 00:33:01 | 检查点 C:owner 管理端批准,0.10 分配至测试条目,订单→paid,用户端同步;总 submitted +0.10,历史 7 笔分毫未动。设计确认:批准不在 reauth 门禁内(门禁覆盖作废/owner 恢复码/恢复邮箱绑定,router.go 有注释) |
| 00:38:02 | 检查点 D:owner 正式作废,原因 `PRODUCTION_WRITE_TEST_20260721`;**reauth 被强制执行**(审计 `admin_reauth_succeeded` 1 条,会话 reauth_at 00:38:02.28 → 作废 00:38:02.62);状态 voided、订单回 submitted、有效金额回基线,作废记录与 payment_item 按设计永久保留 |
| 00:4x | 阶段十二通过;owner 确认**关闭回滚窗口**(云端唯一事实源、本地永久退役、材料保留、未开放客户) |
| 00:41–00:52 | 恢复码生成尝试:仅出现一次 GET(读取状态),无 POST,`admin_recovery_codes` 0 行、生成审计 0 条——**恢复码实际未生成**;owner 决定推迟(密码未忘,后续再处理)。注意:该次尝试中离线保存的内容并非服务器签发的有效恢复码 |

## 最终核验终态(07-21 00:5x 复核)

current→`releases/95036a07911b`(REVISION 完整 SHA,MANIFEST 8/8);库 22/0022;owner=1(admin=active/owner,testadmin=disabled/admin);测试付款 voided(原因带测试标识);历史基线一致(44 单/6318.44,7 笔付款 1145.40/1145.84);测试订单保留 0.10 未付;审计链完整(owner_promoted=1、admin_reauth_succeeded=1、总 234);pjsk-backend/caddy active、NRestarts=0;/health 本地与公网均 200;80/443 正常;8080/5432/5433 公网不可达;本地旧生产保持 Stopped(仅 PG18 归档运行)。

## 测试数据永久留痕说明

以下记录按设计永久保留,不回收:测试用户 `production_write_test_20260721`、测试订单 `IMP-713f5b746cf3-4df85cfa65`(0.10 未付)、1 条 approved 付款提交、1 笔 voided 付款及其分配行、2 条 preview-only 导入批次。它们不影响任何有效业务金额。

## 本地旧生产退役定位

本地 PostgreSQL 18 当前即使仍保持运行,也**仅用于冻结归档保全**:不接收任何业务连接;不作为生产库;不作为热备库;不得直接恢复或切换为生产事实源。如需读取归档数据,必须走单独审批和只读流程。pjsk-caddy 与 pjsk-backend 保持 Stopped,不得重新开放。

## 保留的回滚/审计材料(全部不删除)

- 本地:冻结的 PG18 库(仅归档保全,定位见上节)、final dump(95036a07911b/152434)、旧 historical dump、旧前端/旧 exe;
- 云端:stale 取证备份、中转目录全部文件、旧 release ×2、旧 Caddyfile 备份(含 adapt 快照)、backend.env 备份、MANIFEST/SHA 记录。

## 当前已知待办

1. **owner 恢复码未生成**(owner 决定推迟;生成前"恢复码+SSH CLI"中仅 SSH CLI 可用,且此前离线保存的内容无效,补生成时需替换);
2. HSTS:HTTPS 稳定运行数日后评估启用;
3. SMTP/恢复邮箱、Passkey:后续独立阶段;
4. 本开发日志经人工审阅后提交(不自动提交/推送);
5. "苏归与管理员分级权限系统"(`docs/admin-role-hierarchy-plan.md`):**尚未实现,不得误写为已上线功能**——当前生产仍使用技术角色 owner/admin;页面显示名"苏归"及从用户列表任命管理员的功能均未实现;目前仅完成只读调查、方案设计与决策固化,待审阅后排期开工。

## 风险与后续观察项

- 恢复码缺位期间,owner 账号找回依赖 SSH CLI(`reset-owner-password`),需保证服务器密钥安全;
- 证书自动续期(约 2026-09 下旬首次)需观察一次成功续期;
- Caddy 2.6.2 版本偏旧,后续可单独规划升级(与站点变更分离);
- access.log 增长与滚动策略观察;`/var/log/pjsk` 0751/0644 小偏差沿袭 2G 备忘;
- 首批真实客户使用后关注付款提交图片体量与 25MB 上限;
- 阶段十一"细节问题留待实际使用后优化"事项按反馈跟进。
