# 内网部署密钥生成、轮换与 Windows 保存

本文只提供操作方案。不要把真实密钥发送到聊天、Git、开发日志、工单或截图中。监听地址、可信代理与 CORS 的网络配置见 [internal-network-deployment.md](internal-network-deployment.md)。

## 当前密钥边界

- `RECOVERY_EMAIL_ENCRYPTION_KEY`：标准 Base64，解码后必须正好 32 字节，仅用于 AES-256-GCM 邮箱加密。
- `RECOVERY_EMAIL_HMAC_KEY`：标准 Base64，解码后至少 32 字节，仅用于规范化邮箱盲索引。
- `RECOVERY_EMAIL_VERIFICATION_HMAC_KEY`：标准 Base64，解码后至少 32 字节，仅用于已登录邮箱验证码。
- `QUERY_CODE_RECOVERY_HMAC_KEY`：标准 Base64，解码后至少 32 字节，只作为查询码找回验证码、重置令牌及 CN/IP 标识的根密钥；production 必填，development/test 仅允许临时回退旧验证 HMAC。
- 管理员与普通查询会话不使用上述密钥；会话令牌由 `crypto/rand` 生成 32 字节，数据库只保存 SHA-256。

不同用途的根密钥不得复用。查询码找回内部继续使用版本化 HMAC 上下文区分验证码、重置令牌、CN 标识和 IP 标识即可，不需要为每个上下文增加环境变量。

## 兼容旧 PowerShell 的生成函数

以下代码不能自动写入 `.env`、文件、剪贴板或日志。执行后只在当前 PowerShell 进程中产生变量；显式输出变量时，密钥会出现在终端屏幕上。终端转录、命令录制、远程协助、截图和录屏都可能泄露密钥。

```powershell
function New-SecureBase64Key {
    param(
        [ValidateRange(16, 4096)]
        [int]$Bytes = 32
    )

    $buffer = New-Object byte[] $Bytes
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()

    try {
        $rng.GetBytes($buffer)
        [Convert]::ToBase64String($buffer)
    }
    finally {
        if ($null -ne $rng) {
            $rng.Dispose()
        }

        [Array]::Clear($buffer, 0, $buffer.Length)
    }
}
```

### 模式一：逐个生成和保存（推荐）

每次只生成一个值，立即手动保存到受保护位置，确认保存后清除变量，再生成下一项。下列命令不要在开启 PowerShell Transcript、终端录制、屏幕共享或录屏时执行。

```powershell
$recoveryEmailEncryptionKey = New-SecureBase64Key -Bytes 32
$recoveryEmailEncryptionKey
# 手动保存后立即清除：
Remove-Variable recoveryEmailEncryptionKey -ErrorAction SilentlyContinue

$recoveryEmailLookupHmacKey = New-SecureBase64Key -Bytes 32
$recoveryEmailLookupHmacKey
Remove-Variable recoveryEmailLookupHmacKey -ErrorAction SilentlyContinue

$recoveryEmailVerificationHmacKey = New-SecureBase64Key -Bytes 32
$recoveryEmailVerificationHmacKey
Remove-Variable recoveryEmailVerificationHmacKey -ErrorAction SilentlyContinue
```

查询码找回独立密钥也按 32 字节生成，并与其他三个密钥分开保存：

```powershell
$queryCodeRecoveryHmacKey = New-SecureBase64Key -Bytes 32
$queryCodeRecoveryHmacKey
Remove-Variable queryCodeRecoveryHmacKey -ErrorAction SilentlyContinue
```

### 模式二：一次生成全部（风险更高）

一次生成会让多个密钥同时驻留在进程内存并显示在同一屏幕，只应在隔离终端中使用。四个变量必须生成彼此独立的值。

```powershell
$recoveryEmailEncryptionKey = New-SecureBase64Key -Bytes 32
$recoveryEmailLookupHmacKey = New-SecureBase64Key -Bytes 32
$recoveryEmailVerificationHmacKey = New-SecureBase64Key -Bytes 32
$queryCodeRecoveryHmacKey = New-SecureBase64Key -Bytes 32

[pscustomobject]@{
    RECOVERY_EMAIL_ENCRYPTION_KEY = $recoveryEmailEncryptionKey
    RECOVERY_EMAIL_HMAC_KEY = $recoveryEmailLookupHmacKey
    RECOVERY_EMAIL_VERIFICATION_HMAC_KEY = $recoveryEmailVerificationHmacKey
    QUERY_CODE_RECOVERY_HMAC_KEY = $queryCodeRecoveryHmacKey
}

Remove-Variable recoveryEmailEncryptionKey -ErrorAction SilentlyContinue
Remove-Variable recoveryEmailLookupHmacKey -ErrorAction SilentlyContinue
Remove-Variable recoveryEmailVerificationHmacKey -ErrorAction SilentlyContinue
Remove-Variable queryCodeRecoveryHmacKey -ErrorAction SilentlyContinue
```

`Clear-Host` 只能清理当前显示，不能保证清除终端回滚缓冲、Transcript、录屏或进程内存。保存完成后应关闭该 PowerShell 进程，并按组织要求处理任何受保护备份。

## 轮换原则

- 邮箱 AES 密钥：不得直接替换。旧密文需要旧密钥解密和新密钥重新加密；保留旧密钥直到迁移与恢复验证完成。
- 邮箱查找 HMAC：不得直接替换。必须在不记录明文邮箱的迁移程序内解密、规范化并重算所有盲索引。
- 邮箱验证码 HMAC：轮换会使未完成验证码失效，已验证邮箱状态不受影响；可等待 10 分钟 TTL 或主动失效未完成记录。
- 查询码找回 HMAC：轮换会使未完成验证码、未用令牌和旧 CN/IP 限流关联失效。无缝轮换需要旧密钥短期验证与新旧限流标识联合查询；试运行前可暂停入口并等待 10 分钟流程 TTL 和 1 小时限流窗口。
- 会话：不受上述密钥轮换影响；会话失效由到期、登出、用户状态以及查询码修改/重置等业务操作控制。

## Windows 保存方案

### A. 仓库外、受 NTFS ACL 保护的环境文件

可将正式配置放在类似 `D:\pjsk-secrets\backend.production.env` 的仓库外目录，但当前 Go 程序只调用无参数 `godotenv.Load()`，不会自动读取该路径。必须由受控启动器或服务管理器注入变量，或者后续增加经过审查的显式配置路径支持，不能假设当前版本可直接使用。

目录和文件应关闭继承，只允许专用服务账户及必要管理员读取，普通交互用户不得读取。备份必须加密并有独立访问控制；更新应采用受控替换，确认新文件 ACL 后再移除旧副本，避免编辑器备份、临时文件和回收站残留。

### B. Windows 用户级或系统级环境变量

手工启动时，用户级变量由对应账户的新进程继承；Windows 服务通常需要在服务账户或服务配置中提供变量。系统级变量暴露面更大，不适合默认存放应用密钥。变量变更后必须重启后端进程；同账户或高权限进程仍可能读取进程环境。

### C. Windows 凭据管理器

当前 Go 程序没有读取 Windows 凭据管理器的实现，不能直接使用。支持它需要选择受维护的 Windows 凭据 API/库、定义凭据命名和服务账户权限、处理读取失败与轮换，并补充安装、恢复和测试流程。

### D. NSSM 或 WinSW 注入

适合后续服务化，但密钥可能出现在服务注册表参数、XML 配置或管理界面中。必须使用专用低权限服务账户，并用 NTFS/注册表 ACL 限制配置读取；日志不得打印完整环境。WinSW XML 或 NSSM 导出文件都不能进入 Git或普通备份。

### E. 专门秘密管理服务

单机或小型内网试运行阶段通常过重；多服务器、多人运维、频繁轮换或公网部署时，可提供集中审计、版本、吊销和短期凭据。采用前需要实现应用认证、启动失败策略、缓存和灾难恢复。

## 当前阶段建议

- 最小可行方案：仓库外受 NTFS ACL 保护的配置文件，由仅管理员可读的人工启动包装流程把变量注入当前进程；不得依赖 Go 程序自动读取该文件。正式启用前应把启动方式固化并审查，不应让操作者逐项复制密钥到共享终端。
- 更安全的服务化方案：专用低权限 Windows 服务账户，使用 WinSW 或 NSSM 注入环境，配置文件/注册表严格 ACL，后端和反向代理分离日志权限，并制定轮换重启步骤。
- 后续升级方案：多机或公网阶段迁移到专门秘密管理服务，使用应用身份、审计和版本化轮换；AES/盲索引迁移仍需应用层密钥版本设计。

## SMTP 与 fake sender 的正式环境边界

- `RECOVERY_EMAIL_SENDER_MODE=disabled` 是默认值：后端不构造可用 sender，前端隐藏匿名查询码找回入口，并对已登录用户显示邮件服务暂不可用。管理员仍可登记、替换或解绑邮箱记录，但页面会说明用户当前无法接收验证码。
- `RECOVERY_EMAIL_SENDER_MODE=fake` 仅允许 `APP_ENV=test`，且必须配置验证码 HMAC、不得残留任何 SMTP 字段。`development` 和 `production` 都拒绝 fake，避免测试 sender 被误带入正式运行。
- `RECOVERY_EMAIL_SENDER_MODE=smtp` 在启动加载配置时完成完整性检查，但不会在启动时连接邮件服务器。缺失或非法配置会阻止启动，不会静默降级到 disabled、fake 或明文传输。
- `RECOVERY_EMAIL_SMTP_TLS_MODE` 只能为 `tls`（连接即 TLS）或 `starttls`（服务端必须声明并成功升级）；两者都启用主机名/证书链校验，最低 TLS 1.2，不提供跳过证书校验的配置。
- `RECOVERY_EMAIL_SMTP_USERNAME` 与 `RECOVERY_EMAIL_SMTP_PASSWORD` 必须同时存在或同时为空。允许同时为空只用于经过控制的内网匿名 relay；这不是默认推荐方案，必须由网络 ACL、relay 白名单和最小出站范围共同限制。
- `RECOVERY_EMAIL_SMTP_FROM` 必须是单一规范地址。`RECOVERY_EMAIL_SMTP_FROM_NAME` 禁止换行且最多 128 个 UTF-8 字节，用户名同样禁止换行，防止邮件头注入。
- 当前发送器不自动重试，每次请求最多建立一次 SMTP 会话；连接、TLS、认证、收件人、DATA 或 QUIT 失败均只返回通用不可用错误，不回显主机、端口、账号、密码、收件人或服务器响应。
- 单次发送默认硬上限 10 秒；若 HTTP 请求 deadline 更早则采用更早值。请求取消会主动关闭连接。上线时还应在防火墙限制后端只访问批准的 SMTP 地址和端口。
- 已登录邮箱验证邮件与匿名查询码重置邮件使用不同方法、fake purpose、标题和正文。两者仅包含本次验证码、UTC 过期时间和用途说明，不包含完整账户资料、查询码、Cookie、会话令牌、数据库信息或密钥。

### 正式启用顺序

1. 先在仓库外的受保护配置中准备完整 SMTP 字段，保持仓库内示例值为空；不要编辑或提交真实 `.env`。
2. 用隔离的非生产收件箱和经过批准的 SMTP 测试环境验证证书链、STARTTLS/TLS 模式、认证、发件人策略与退信处理。本阶段代码检查不连接真实邮件服务器。
3. 确认反向代理和 Windows 服务的超时大于应用单次发送上限，并限制服务账户的出站网络。
4. 将 sender mode 从 disabled 切换为 smtp 后重启后端；检查 `/api/config` 仅显示邮件投递是否可用，不暴露 SMTP 细节。
5. 若投递异常，回退到 disabled 并重启。不要切换到 fake，也不要降低 TLS 或关闭证书校验。