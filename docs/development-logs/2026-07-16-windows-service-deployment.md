# Windows 服务化部署执行记录（本轮：构建与 ACL 加固完成，服务安装因缺少可信 WinSW 与管理员权限而停止）

> 安全边界：本文不记录任何密钥值、数据库密码、完整连接串或 `backend/.env` 内容。构建产物 SHA-256 允许记录。

## 执行前 Git 基线

- 执行日期：2026-07-16（Asia/Shanghai）。
- 分支 `main`；`HEAD` 与 `origin/main` 均为 `dfe746a87da2248d28f151d86bf32e97e0efdcfe`；工作区、暂存区、未跟踪文件均为空。

## 阶段 1：只读部署调查

- 系统：Windows 10 专业工作站版（10.0.19045），AMD64；Go 1.26.5。
- 部署文档：`docs/windows-service-deployment.md` 明确首选 WinSW（配置可审查、支持失败重启与日志滚动），NSSM 仅备选；服务 id 规定为 `pjsk-backend`；明确禁止自动下载包装器二进制。
- 仓库内与服务相关文件：仅 `deploy/windows-service/backend-winsw.xml.example` 与 `caddy-winsw.xml.example` 两个占位示例，无任何 WinSW/NSSM 可执行文件。
- 本机搜索：PATH（`where.exe`）、scoop、chocolatey、常见工具目录、C:\ 与 D:\ 根目录均未发现 winsw/nssm 可执行文件。
- 权限：当前进程以 `sugui\苏归` 运行，UAC 过滤令牌（BUILTIN\Administrators 为 deny-only），`IsInRole(Administrator)=False`，本会话无法注册 Windows 服务，也无法交互式提权。
- 端口与进程：8080 无监听；无 pjsk 相关服务或计划任务；无遗留后端进程。
- PostgreSQL 服务：`postgresql-x64-18`，Running，Automatic——后续 WinSW `<depend>` 应使用该名称（与 XML 示例注释中的占位名一致）。
- `backend/.env`：存在；被根 `.gitignore` 的 `.env` 规则排除；所有者 `SUGUI\苏归`；修改前 ACL 见下文。未读取任何变量值。
- 服务账户评估：项目无既有专用服务账户；文档推荐专用低权限本地账户（本轮禁止创建账户/设置密码，需用户决定）；备选 `LocalService`（通过 Authenticated Users 间接获得的读取权限在本轮 ACL 收紧后已失效，正式安装时需对 `.env`、exe、日志目录显式授权只读/执行/写入）。

## 部署方式决策与停止点

- 决策：按文档采用 WinSW + 固定编译产物 `backend\bin\pjsk-backend.exe`、工作目录含受保护 `.env`、Automatic 启动、失败重启退避、依赖 `postgresql-x64-18`。
- **停止安装的原因（任务第十二节触发项）**：
  1. 全机不存在可信 WinSW/NSSM 可执行文件，且禁止自动下载；
  2. 当前会话无管理员权限，`sc`/`winsw install` 必然失败。
- 本轮未安装、未注册、未启动任何 Windows 服务；未创建账户；未修改防火墙；未使用 LocalSystem。
- 可信获取方案（留待人工执行）：从 WinSW 官方 GitHub Releases（github.com/winsw/winsw）下载对应 x64 版本，核对官方发布的哈希后放置到本机部署目录（如 `D:\pjsk\runtime\winsw\`，该目录不进 Git），复制 `backend-winsw.xml.example` 为正式 XML 并替换占位值（executable 指向 `D:\pjsk\backend\bin\pjsk-backend.exe`、workingdirectory 指向存放受保护 `.env` 的目录、depend 填 `postgresql-x64-18`），再在管理员 PowerShell 中 `winsw install/start`。

## 构建发布产物（已完成）

- 发布前检查：`gofmt -l` 输出为空；`go build ./...`、`go vet ./...`、`go test -count=1 ./...` 全部通过（集成测试遵循既有隔离门禁，未触正式库）。
- 构建命令：`backend` 目录 `go build -trimpath -o .\bin\pjsk-backend.exe .`（文档 §5 标准方式）。
- 产物：
  - 路径：`D:\pjsk\backend\bin\pjsk-backend.exe`
  - 大小：17,135,104 字节
  - 构建时间：2026-07-16 10:13:18（本机时间）
  - SHA-256：`49C9CE641C35F6D3B04E0D44D9D0F38787E088A258E506CC7F7C7AC38785C754`
- Git 忽略确认：`git check-ignore` 命中 `.gitignore:43 backend/bin/`（另有 `backend/*.exe` 规则双重覆盖），产物不会进入 Git。
- 产物冒烟测试：以当前用户从 `backend` 工作目录直接运行 exe，日志 `database connected`、`backend listening on 127.0.0.1:8080`，`GET /health` 返回 HTTP 200、`database=connected`，无迁移重放、无 HMAC/数据库错误；测试后进程已停止、8080 已释放。
- 运行目录内无源码临时文件、测试输出或敏感副本;未复制任何 `.env`。

## backend/.env ACL 加固（已完成）

- 修改前 ACL（全部为目录继承，仅记录主体与权限，未备份文件内容）：
  - 两个无法解析的孤儿 SID：Modify
  - `SUGUI\CodexSandboxUsers`：Modify
  - `BUILTIN\Administrators`：FullControl
  - `NT AUTHORITY\SYSTEM`：FullControl
  - `NT AUTHORITY\Authenticated Users`：Modify
  - `BUILTIN\Users`：ReadAndExecute
- 修改方式：`icacls /inheritance:r /grant:r`，仅针对该文件，未递归、未触碰目录或其他文件。
- 修改后 ACL（全部显式）：
  - `NT AUTHORITY\SYSTEM`：FullControl
  - `BUILTIN\Administrators`：FullControl
  - `SUGUI\苏归`：Modify
- 已移除：Authenticated Users、Users、CodexSandboxUsers 及两个孤儿 SID 的全部访问。
- 验证：当前用户可正常打开读取（未显示内容）；新 ACL 下后端 exe 再次启动成功、`/health` HTTP 200、`database=connected`，随后停止、端口释放。
- 服务账户权限说明：最终服务运行身份尚未确定（需用户决定专用账户或 LocalService），本轮未预授任何服务身份;正式安装时应对选定身份追加对该文件的**只读**授权（不得给修改权），例如 `icacls D:\pjsk\backend\.env /grant "<服务身份>:(R)"`。

## 数据库只读复核

- psql 显式只读事务：`schema_migrations` 共 19 条，最高 `0019_admin_auth_audit_events.sql`，`0019` 恰好 1 条。未执行任何迁移，未重放 0019。

## 服务验证项状态

- 服务安装/启动类型/启动/停止/重启/异常恢复测试：**均未进行**（服务未安装，见停止点）。
- 真实开机自启验证：**未进行真实开机验证**（本轮未安装服务，也未重启计算机）。
- 邮件发送：仍为 disabled；未配置 `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`、未配置 SMTP、未发送任何邮件、未修改恢复邮箱或查询码。

## 结论、遗留风险与下一步

- 本轮完成：发布产物构建与验证、`backend/.env` ACL 收紧并复验、数据库状态复核；未修改任何业务代码或配置模板。
- 遗留风险 1：后端仍无持久化运行机制（服务未安装），主机重启后不会自动运行。
- 遗留风险 2：项目目录整体 ACL 仍较宽（本轮仅收紧 `.env` 单个文件，未递归调整）。
- 遗留风险 3：服务运行身份未定——需用户在"专用低权限本地账户（文档推荐，需人工创建并设置密码）"与"内置 LocalService（无需密码，但需逐项显式授权）"之间决策。
- 下一步（人工，需管理员权限）：
  1. 从 WinSW 官方 Releases 获取并核验 WinSW x64 可执行文件；
  2. 决定服务运行身份；
  3. 按本文"可信获取方案"完成 XML 配置与 `winsw install`，为服务身份授予 exe 执行、`.env` 只读、日志目录写入权限；
  4. 安装后按任务清单验证启动/停止/重启/失败恢复与 `/health`，并择机重启主机验证真实开机自启。
- 子代理：本轮未使用任何子代理、子任务或并行代理。
