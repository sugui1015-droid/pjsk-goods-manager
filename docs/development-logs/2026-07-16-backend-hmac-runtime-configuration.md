# 后端 QUERY_CODE_RECOVERY_HMAC_KEY 运行时配置与验证记录

> 安全边界：本文不记录任何密钥值、数据库密码、连接串或业务明细。密钥仅写入 Git 之外的本机配置，全程未在终端回显、未进入命令历史明文、未写入任何 Git 跟踪文件。

## 阶段 1：配置加载调查（只读）

- 执行日期：2026-07-16（Asia/Shanghai）。
- 起点 Git 状态：`HEAD` 与 `origin/main` 均为 `369e7221c2553696c7267225e48910371952c328`，工作区、暂存区、未跟踪文件均为空。
- 配置加载入口：`backend/internal/config/config.go` 的 `Load()` 调用无参数 `godotenv.Load()`，只读取进程当前工作目录下的 `.env`（标准启动在 `backend` 目录运行，即 `backend/.env`），其余全部取进程环境变量。不读取仓库根 `.env`、Windows 凭据管理器或其他路径。
- `QUERY_CODE_RECOVERY_HMAC_KEY` 要求：标准 Base64，解码后至少 32 字节；`APP_ENV=production` 必填；development/test 仅允许回退 `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`（本机同样未配置，故此前启动失败）。
- 配置现状：`backend/.env` 存在且被根 `.gitignore` 的 `.env` 规则排除（`git ls-files` 确认仅跟踪 `.env.example`）；文件内已有 `SERVER_PORT=8080`、`DATABASE_URL`、`RECOVERY_EMAIL_ENCRYPTION_KEY`、`RECOVERY_EMAIL_HMAC_KEY` 四个变量（仅核对变量名，未读取值）；无 `QUERY_CODE_RECOVERY_HMAC_KEY` 行；无 `APP_ENV`（默认 development）。
- 环境变量：Process、User、Machine 三个作用域均未设置 `QUERY_CODE_RECOVERY_HMAC_KEY`，确认不存在既有有效密钥，无覆盖风险。
- 运行方式：无 pjsk 相关 Windows 服务或计划任务；8080 端口无监听；无遗留 `pjsk-backend`/`go` 进程。`docs/windows-service-deployment.md` 与 `deploy/windows-service` 仅为文档/示例，未实际部署。当前唯一标准启动方式为 `backend` 目录 `go run .`。

## 阶段 2：配置方式选定

- 采用优先级最高的既有方案：把密钥追加到 `backend/.env` —— 项目既有、被 `.gitignore` 明确排除、已存放其他根密钥与数据库连接串、且是 godotenv 实际加载的位置。未新建仓库外 secrets 文件，未修改环境变量，未改动 Git 跟踪文件。
- ACL 说明：`backend/.env` 权限全部继承自项目目录（含 Authenticated Users 可修改、Users 可读）。本轮未重写该文件 ACL：文件中既有密钥即在此权限下存放，且本机存在沙箱用户组参与运行，收紧继承权限可能破坏现有流程。收紧目录/文件 ACL 列为遗留风险，建议由所有者统一处理。

## 阶段 3：密钥生成与写入

- 生成方式：`System.Security.Cryptography.RandomNumberGenerator::Create()` 实例方法（避开本机不支持的静态 `GetBytes` 调用），32 随机字节，标准 Base64 编码（44 字符）。
- 写入方式：单条 PowerShell 命令内生成并直接追加 `QUERY_CODE_RECOVERY_HMAC_KEY=<值>` 到 `backend/.env`（ASCII 追加，自动补齐前置换行），命令文本本身不含密钥值；写入后立即 `Remove-Variable` 并 `[Array]::Clear` 清除明文变量；全程未回显、未写剪贴板、未写普通临时文件。
- 生成前校验拒绝全零字节与异常长度；实际结果非全零。

## 阶段 4：启动前验证

- `backend/.env` 中该变量恰好 1 行；Base64 长度 44；解码成功且为 32 字节，满足 ≥32 字节要求。
- `git status --short` 为空；`git diff` 为空；无未跟踪文件——密钥未进入 Git 视野。
- 正式库只读复核（psql `BEGIN TRANSACTION READ ONLY` + `ROLLBACK`）：`schema_migrations` 共 19 条，最低 `0001_core_tables.sql`，最高 `0019_admin_auth_audit_events.sql`，`0019` 恰好 1 条。未执行任何迁移。

## 阶段 5：启动后端

- 启动方式：项目标准入口，`backend` 目录 `go run .`；未设置任何临时/伪造密钥，配置完全来自 `backend/.env`。
- 启动日志：无 `.env not loaded` 告警（.env 加载成功）；`database connected`；直接进入 `backend listening on 127.0.0.1:8080`；没有任何 `migration applied` 行（迁移器识别全部已执行，未重放 0019）；无 HMAC 配置错误、无数据库错误、无端口冲突。

## 阶段 6：健康与功能验证

- `GET http://127.0.0.1:8080/health`：HTTP 200，`status=ok`、`database=connected`、`service=pjsk-backend`。
- 启动后再次只读复核：仍为 19 条 / 最高 `0019_admin_auth_audit_events.sql` / `0019` 恰好 1 条。
- 全程后端日志仅三行：connected、listening、一次 `GET /health 1ms`；无重复迁移、无错误。
- 邮箱找回专项测试（隔离/单元测试，不触正式库、不发真实邮件）：`go test -count=1 ./internal/querycoderecovery ./internal/recoveryemailverification ./internal/recoveryemail ./internal/config` 全部 ok。
- `go build ./...`：通过。`go vet ./...`：通过。`go test -count=1 ./...`：17 个含测试包全部 ok，4 个包无测试文件，无失败；数据库集成测试遵循既有隔离门禁，未连接正式库。
- 未创建真实找回请求，未修改任何用户恢复邮箱或查询码，未向外部邮箱发送验证码（sender mode 保持默认 disabled）。

## 阶段 7：运行方式确认

- 当前后端仅在本次会话中临时运行用于验证，验证完成后已主动停止进程（8080 已释放，无残留 `backend`/`go` 进程）。**正式持久化部署尚未建立**：无 Windows 服务、无计划任务、无常驻启动脚本。
- 本轮按边界要求未创建服务或计划任务。下一步建议：按 `docs/windows-service-deployment.md` 以专用低权限服务账户 + WinSW/NSSM 建立持久运行方式，注意服务环境变量注入与 `backend/.env`/服务配置的 ACL 收紧。

## 结论与遗留风险

- `QUERY_CODE_RECOVERY_HMAC_KEY` 配置缺口已消除：真实密码学随机密钥已持久化到 Git 之外的既有本机配置，后端可正常启动并通过健康检查。
- 未重新执行 0019，未手工执行迁移 SQL，未修改业务数据，未修改任何代码或配置模板，未使用子代理。
- 遗留风险 1：`backend/.env` 及项目目录的 NTFS 权限较宽（Authenticated Users 可修改），建议所有者统一收紧敏感配置的 ACL。
- 遗留风险 2：后端仍无持久化运行机制，服务器重启或终端关闭后不会自动运行，正式上线前需建立服务化启动。
- 遗留风险 3：`RECOVERY_EMAIL_VERIFICATION_HMAC_KEY` 与 SMTP 尚未配置，邮箱验证码发送功能保持 disabled；启用前按 `docs/internal-deployment-secrets.md` 的正式启用顺序处理。
- 子代理：本轮未使用任何子代理、子任务或并行代理。
