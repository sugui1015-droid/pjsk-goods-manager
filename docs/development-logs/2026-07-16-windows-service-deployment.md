# Windows 服务化部署执行记录（本轮：构建与 ACL 加固完成，服务安装因缺少可信 WinSW 与管理员权限而停止）

> 更新（2026-07-16 同日第二轮）：后续会话已完成正式部署。WinSW v3 系列二进制以 LocalService 启动失败（取证见文末"第二轮执行记录"），最终改用 NSSM 部署成功，`pjsk-backend` 服务已 Running 并通过启停/异常恢复验收。本文前半部分保留第一轮当时的事实与结论，不作改写。

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

---

# 第二轮执行记录（2026-07-16 同日后续）：WinSW v3 启动失败取证与 NSSM 部署成功

> 执行模式：Claude 会话本身无管理员权限；所有需要管理员的操作均为"Claude 生成命令 → 用户在管理员 PowerShell 手工执行 → 输出回传逐项审核"。构建、数据库只读核对与文档修改在普通权限会话完成。安全边界与第一轮相同：未读取、显示或记录 `.env` 内容与任何密钥；未启用 SMTP、未发送邮件；未执行、未重放任何迁移；未修改业务代码与业务数据；未使用子代理。

## 1. WinSW 获取与核验

- WinSW 由用户人工获取并放置于仓库外 `D:\PJSK-Tools\WinSW\WinSW-x64.exe`（曾短暂位于仓库内 `D:\pjsk\PJSK-Tools\`，只读验证发现后由用户在管理员会话移出，`git status` 恢复干净）。
- 核验：18,286,774 字节；SHA-256 `A2DAA6A33A9C2B791AE31D9092E7935C339D1E03E89BFB747618CE2F4E819E20`；ProductName "Windows Service Wrapper"（CloudBees, Inc.）；文件版本 3.0.0.0，`--version` 输出 `3.0.0+a6ba41681d84d84d95eb7a377c369d709e32225b`；无 Authenticode 签名（该项目官方发布本身即未签名）。v3 官方未附校验和文件，哈希由用户人工核对下载来源。

## 2. 仓库外服务目录、日志目录与目录 ACL

| 路径 | 用途 | 最终 ACL（`/inheritance:r` 后全显式） |
| --- | --- | --- |
| `D:\PJSK-Service\backend` | 服务包装器与配置 | SYSTEM `(OI)(CI)F`、Administrators `(OI)(CI)F`、LOCAL SERVICE `(OI)(CI)RX` |
| `D:\PJSK-Runtime\logs\backend` | 服务运行日志 | SYSTEM `(OI)(CI)F`、Administrators `(OI)(CI)F`、LOCAL SERVICE `(OI)(CI)M`（滚动需重命名/删除旧日志，M 不含更改权限/取得所有权） |

- 已验证的 WinSW 复制为 `D:\PJSK-Service\backend\pjsk-backend-service.exe`（副本 SHA-256 与源一致）。
- ACL 命令一律用 SID（`*S-1-5-18`/`*S-1-5-32-544`/`*S-1-5-19`）避免本地化差异；均经 `icacls` 输出独立复核，不只看退出码。

## 3. 发布产物复验与重建

- 发布前检查：`gofmt -l` 空输出；`go build ./...`、`go vet ./...` 退出码 0；`go test -count=1 ./...` 19 个包全部 ok。
- 重建：`go build -trimpath -o bin\pjsk-backend.exe .`。
- 产物：`D:\pjsk\backend\bin\pjsk-backend.exe`，17,135,104 字节，构建时间 2026-07-16 11:05:34，SHA-256 `2D61520508C6A68180E9985A1BB69A2F1F26D31E8605300E0FC0469436F5FA31`。
- 与第一轮产物哈希（`49C9CE…`）不同属预期：Go 将 VCS 提交号嵌入产物，HEAD 已由 `dfe746a` 前进至 `2922537`；大小不变。
- `git check-ignore` 仍命中 `.gitignore:43 backend/bin/`；产物不进 Git，工作区无新增文件。
- 顺序说明：重建会删除重建 exe 并丢失其上的显式 ACE，因此本轮固定"先构建、后授 ACL"。

## 4. LocalService 最小授权（ACL 六阶段之收尾）

- 现状发现：`D:\pjsk` 整树继承 `Authenticated Users:Modify`，LOCAL SERVICE 作为其成员本已间接可读写（遗留宽 ACL，本轮不递归收紧）。显式授权的意义是声明最小意图并在未来整树收紧时不致断服。
- 非递归追加（`/grant`，不带 `:r`）：`D:\pjsk\backend` 目录本身 `(RX)`、`D:\pjsk\backend\bin` 目录本身 `(RX)`、`pjsk-backend.exe` `(RX)`。
- `backend\.env` 追加 LOCAL SERVICE `(R)`，最终恰好四条显式 ACE：SYSTEM `(F)`、Administrators `(F)`、`SUGUI\苏归` `(M)`、LOCAL SERVICE `(R)`——只读，无写入/删除/更改权限/取得所有权。`icacls` 复核通过；未输出 `.env` 内容。

## 5. WinSW 正式 XML 与安装前门禁（当时全部通过）

- 生成 `D:\PJSK-Service\backend\pjsk-backend-service.xml`：1,699 字节，SHA-256 `E812FC1E02741CDE2044D737C9F83B7F3F2DFDE68A796C736CA9E6DAF3DF2D07`。字段：id `pjsk-backend`、name `PJSK Goods Manager Backend`、executable `D:\pjsk\backend\bin\pjsk-backend.exe`、workingdirectory `D:\pjsk\backend`、serviceaccount `NT AUTHORITY\LocalService`、Automatic + delayedAutoStart、depend `postgresql-x64-18`、logpath `D:\PJSK-Runtime\logs\backend`（roll-by-size 10 MB × 8）、stoptimeout 20 sec、onfailure restart 10s/30s/60s + resetfailure 1 hour。有意不含任何 `<env>`（配置统一由工作目录受保护 `.env` 提供，避免 XML 静默覆盖）。敏感信息扫描仅命中注释中的英文单词，无任何值。
- 安装前门禁 11 项全过：Git 干净、`.env` 不在 Git、服务目录在仓库外、XML 无敏感信息、8080 空闲、服务不存在、PostgreSQL Running、LocalService 对 `.env` 只读、对日志目录可写、后端冒烟（以当前用户直跑：`database connected`、`listening on 127.0.0.1:8080`、`/health` 200、无迁移重放）、数据库显式 READ ONLY 事务核对 19 条/最高 `0019_admin_auth_audit_events.sql`/`0019` 恰 1 条。

## 6. WinSW 安装成功、service mode 启动失败（已卸载，证据保留）

- 安装（bundled 模式）成功，SCM 配置逐项核验正确：`AUTO_START (DELAYED)`、`SERVICE_START_NAME : NT AUTHORITY\LocalService`、`DEPENDENCIES : postgresql-x64-18`、故障恢复 RESTART 10000/30000/60000 ms、RESET_PERIOD 3600。
- 启动失败时间线（wrapper 日志 + 事件日志）：
  - 2026-07-16 11:19:01 安装成功（管理员令牌下执行，正常）。
  - 11:20:34.788 SCM 以 LocalService 启动包装器，`Starting WinSW in service mode.`
  - 11:20:34.885（约 97 ms 后）`FATAL — Failed to open the service. 拒绝访问。`
  - SCM 事件 7009（等待连接超时 85000 毫秒）与 7000（启动失败）。
  - 11:24:12 卸载成功。
- 取证结论：失败发生在后端子进程启动之前——后端无任何启动日志（out/err 日志从未生成，仅有 wrapper.log）、8080 未监听、数据库未被改变（复核仍为 19/0019/1）。同一 exe 同一 `.env` 冒烟正常、wrapper 日志可正常写入日志目录，说明后端产物、`.env` ACL、日志目录授权均无问题，失败点在包装器自身初始化。
- 根因：与"本次使用的 WinSW v3 系列二进制（文件版本 3.0.0.0）在受限服务账户下打开自身服务对象时请求过高访问权限"的已知问题一致（参见 https://github.com/winsw/winsw/issues/872 ，受限账户对自身服务对象默认无完全访问权，`OpenService` 即被拒）。
- 明确未采用的绕过（安全否决）：扩大服务 SDDL、授予服务账户 SERVICE_ALL_ACCESS、改用 LocalSystem——均等于放弃最小权限，未执行。
- 证据保留（仓库外，不删除、不入 Git）：`D:\PJSK-Service\backend\pjsk-backend-service.exe`（WinSW 副本）、`pjsk-backend-service.xml`、`D:\PJSK-Runtime\logs\backend\pjsk-backend-service.wrapper.log`、`D:\PJSK-Tools\WinSW\WinSW-x64.exe`。

## 7. NSSM 调查、下载核验

- 版本选择：nssm.cc 官方明确 Windows 10 Creators Update 及更新系统应使用预发布 2.24-101（稳定版 2.24 为 2014 年发布，在新系统有服务无法启动的已知问题），故采用 **2.24-101-g897c7ad**（2017-04-26）。
- 下载包 `nssm-2.24-101-g897c7ad.zip`（用户人工从 nssm.cc 下载）：SHA-256 `99F5045FFFBFFB745D67FE3A065A953C4A3D9C253B868892D9B685B0EE7D07B8`（与 Microsoft winget 官方清单一致）；SHA-1 `CA2F6782A05AF85FACF9B620E047B01271EDD11D`（与 nssm.cc 官方公布一致）——双独立来源交叉核验。
- 采用二进制：zip 内 `win64\nssm.exe`，版本 2.24-101-g897c7ad，368,640 字节，SHA-256 `EEE9C44C29C2BE011F1F1E43BB8C3FCA888CB81053022EC5A0060035DE16D848`。
- 部署位置：`D:\PJSK-Service\backend\nssm.exe`（继承该目录既有 ACL，LOCAL SERVICE 得 RX，无需新增 ACL）。
- NSSM 不受 WinSW 失败机制影响：nssm.exe 作为服务进程只注册控制处理器、读取自身 Parameters 注册表键并拉起子进程，不需要以完全访问打开自身服务对象；LocalService 是其常规用法。

## 8. NSSM 安装与最终配置（实际生效值）

| 项 | 值 |
| --- | --- |
| 服务名称 / 显示名称 | `pjsk-backend` / `PJSK Goods Manager Backend` |
| SCM 包装器 | `D:\PJSK-Service\backend\nssm.exe` |
| Application | `D:\pjsk\backend\bin\pjsk-backend.exe` |
| AppDirectory | `D:\pjsk\backend`（后端从此目录读取受保护 `.env`） |
| 运行账户 | `NT Authority\LocalService` |
| 启动类型 | `SERVICE_AUTO_START`（Auto；未配置延迟启动） |
| 依赖服务 | `postgresql-x64-18` |
| AppExit Default / AppRestartDelay / AppThrottle | Restart / 5000 ms / 10000 ms |
| stdout / stderr | `D:\PJSK-Runtime\logs\backend\backend-out.log` / `backend-err.log` |
| 日志滚动 | `AppRotateFiles=1`、`AppRotateOnline=1`、`AppRotateSeconds=86400`、`AppRotateBytes=10485760` |

- 未把任何敏感环境变量写入 NSSM 注册表参数；后端继续从工作目录下受 ACL 保护的 `.env` 加载全部配置。
- 后端使用 Go 标准库日志，实际运行日志进入 stderr：`backend-out.log` 为空属正常，运行日志看 `backend-err.log`。

## 9. ACL 门禁复验（LocalService 实际可用性）

实际核验通过：LocalService 可读取 `D:\pjsk\backend`、可读取并执行 `pjsk-backend.exe`、可读取 `D:\pjsk\backend\.env`（仅只读）、可修改 `D:\PJSK-Runtime\logs\backend`。`.env` 内容未输出、未提交。

## 10. 首次启动结果

- 服务 Running；`/health` HTTP 200；后端进程 1 个；8080 监听 1 个；后端 PID 与监听 PID 一致。
- 首次运行日志（stderr）：`database connected`、`backend listening on 127.0.0.1:8080`、`GET /health`。无权限错误、端口冲突、HMAC 或数据库错误；无任何 `migration applied` 行（启动未重复执行迁移）。

## 11. 正式数据库显式只读复核

- `BEGIN TRANSACTION READ ONLY`（`transaction_read_only = on`）下核对：迁移总数 19；最新 `0019_admin_auth_audit_events.sql`；`0019` 记录数 1；`admin_auth_audit_events` 表存在，索引 5 个，当前 208 行。
- 复核前后 `/health` 均 HTTP 200。本阶段未执行任何数据库写入；服务启动未重复执行迁移；数据库状态保持 19 / 0019 / 1。

## 12. 停止、重新启动与异常恢复验收（全部通过）

以下 PID 是本次验收时的运行证据，非固定配置值：

- 测试前基线：Running，NSSM PID 10680，后端 PID 20976，health 200。
- 受控停止：Stopped；后端进程 0；8080 监听 0；health 不可访问。通过。
- 受控重新启动：Running；NSSM PID 22020，新后端 PID 20460；health 200；启动耗时约 10.56 秒。通过。
- 子进程异常退出测试：仅强制终止后端 PID 20460 一次（未动 NSSM 与 PostgreSQL）；NSSM（PID 22020 保持不变）按 `AppExit Restart + AppRestartDelay 5000ms` 自动拉起新后端 PID 19516，恢复耗时约 15.91 秒；health 200。通过。
- 最终：Running，NSSM PID 22020，后端 PID 19516，8080 监听 PID 19516，health 200。本阶段未修改数据库。
- 真实开机自启：**未验证**（未重启计算机）；仅确认启动类型为 Auto 且已声明 PostgreSQL 依赖。

## 13. 文档收尾轮只读终态确认（本次会话）

- Git：`main`，HEAD 与 `origin/main` 提交前均为 `29225377f15197725e2f2937e5954c5645bc738e`，工作区干净（文档修改前）。
- 服务：Running，`StartName = NT Authority\LocalService`，`PathName = D:\PJSK-Service\backend\nssm.exe`；后端 PID 19516；`127.0.0.1:8080` LISTEN（OwningProcess 19516）；`/health` HTTP 200、`database=connected`。未对服务做任何操作。

## 14. 邮件与安全边界（保持不变）

- 邮件发送仍为 disabled：未配置 SMTP、未配置邮件验证相关 HMAC 密钥变量、未发送任何邮件、未创建验证码、未修改恢复邮箱或查询码。
- 全程未使用 LocalSystem、未创建 Windows 账户、未改防火墙/hosts/系统环境变量、未强制推送、未使用子代理、未修改业务代码。

## 15. 遗留风险与下一步

1. 启动类型为 Auto（未配置延迟启动，与第一轮 WinSW 方案的 delayed 设计不同）；依赖 `postgresql-x64-18` 已声明，由 SCM 保证顺序。如需延迟启动可后续单独调整。
2. NSSM 日志滚动无"保留份数"上限，`D:\PJSK-Runtime\logs\backend` 旧日志会累积，需定期人工或计划任务清理。
3. 重启策略为固定 5 秒延迟 + 10 秒节流，非 WinSW 式递增退避；数据库长期不可用时会在节流限制下周期性重启。
4. `D:\pjsk` 整树 `Authenticated Users:Modify` 仍宽（第一轮已登记的遗留风险，本轮未递归收紧）。
5. 未真实重启主机验证开机自启。
6. `pjsk-caddy` 反代服务仍未部署（HTTPS/静态前端仍待办）。
7. 运维红线：旧 WinSW 证据文件仍在服务目录内，**不得启动旧 WinSW 包装器**（会与 NSSM 服务争抢 8080）；升级后端应 `Stop-Service pjsk-backend` → 备份现役 exe → 替换 → 启动 → `/health` 复查。
8. NSSM 2.24-101 为 2017 年预发布、二进制未签名——已双源哈希核验作为缓解；后续如出现维护版本可再评估。
