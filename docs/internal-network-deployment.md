# 内网部署：监听、可信代理与 CORS

本文只描述配置语义和推荐结构，不包含真实域名、真实内网 IP、真实证书、真实代理配置或安装命令。密钥与 SMTP 边界见 [internal-deployment-secrets.md](internal-deployment-secrets.md)。

## 推荐结构

```text
内网客户端
    ↓ HTTPS
内网域名或内部 DNS
    ↓
本机 Nginx/Caddy
    ↓
Go 后端 127.0.0.1:8080
    ↓
PostgreSQL 127.0.0.1:5432
```

前端静态文件与 `/api` 由同一个反向代理站点提供（同源），因此正式部署通常不需要配置任何 CORS 跨域来源。

## 监听地址：`SERVER_HOST`

- 默认 `127.0.0.1`，后端只监听本机回环地址；这是与旧版本（监听全部接口）不同的**有意安全行为变化**。原先局域网直接访问 `http://<本机IP>:8080` 的方式默认失效。
- 只有显式设置 `SERVER_HOST=0.0.0.0` 或 `SERVER_HOST=::` 才监听全部接口；production 使用全接口监听会在启动日志输出一次安全提醒。
- 只接受字面 IPv4/IPv6 地址：不允许 hostname（包括 `localhost`）、不允许带端口、不允许 CIDR 或 zone；非法值启动失败。
- 端口优先级不变：`APP_PORT` > `SERVER_PORT` > `BACKEND_PORT` > `8080`。IPv6 绑定地址由 `net.JoinHostPort` 拼接（如 `[::1]:8080`）。

## 可信代理：`TRUSTED_PROXY_CIDRS`

- 默认空：**不信任任何代理头**，客户端 IP 一律取 TCP 连接来源；`X-Forwarded-For` 完全被忽略。`X-Real-IP` 和 `Forwarded` 在任何配置下都不被支持。
- 逗号分隔 CIDR，支持 IPv4 与 IPv6。示例（代理与后端同机时的推荐值）：

  ```text
  TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128
  ```

- 不要把整个内网网段配置为可信代理，除非代理确实可能从该网段直接连接后端。配置越窄，伪造面越小。
- 裸 IP、hostname、非法掩码、中间空项、`0.0.0.0/0`、`::/0` 都会让启动失败；重复 CIDR 规范化后去重。
- 只有当 TCP 连接来源落入可信 CIDR 时才解析 `X-Forwarded-For`，并从链的最右侧向左剥离可信代理节点，第一个不可信合法地址即客户端 IP；链中出现任何非法条目（端口、hostname、`unknown`、空项、zone）、超过 4096 字节或 32 个地址时，整条链作废并回退到连接来源地址。
- **反向代理必须覆盖客户端带来的原始 `X-Forwarded-For`**（或至少追加真实连接地址）；不要配置成盲目透传客户端头，否则右向左剥离虽然能保住不可信边界，但代理自身语义就不完整了。
- 无法解析的连接来源统一落入 `unknown-client` 限流桶：所有异常来源共享同一份限流，这是有意的安全失败行为。

## CORS：`CORS_ALLOWED_ORIGINS`

- development/test 未配置时默认允许 `http://localhost:5173` 与 `http://127.0.0.1:5173`（本地 Vite）。
- **production 未配置时默认没有任何跨域来源**。推荐前端与 API 同源部署，此时无需配置本变量。
- 显式配置时：逗号分隔，每项必须是精确的 `http://` 或 `https://` origin（可带端口）；禁止 `*`、`null`、path、query、fragment、用户名密码；不做前缀匹配；`:80`/`:443` 默认端口不折叠，以配置的精确字符串为准；尾部 `/` 会被规范化移除；重复项与非法项启动失败。
- 响应行为：只对已验证允许的来源返回精确 `Access-Control-Allow-Origin` 与 `Access-Control-Allow-Credentials: true`，并追加 `Vary: Origin`；预检只放行前端真实使用的方法（GET/POST/PUT/PATCH/DELETE/OPTIONS）与 `Content-Type` 头；不允许的来源不返回任何允许头。
- Cookie 规则（`HttpOnly`、`SameSite=Lax`、`ADMIN_COOKIE_SECURE`）不受本配置影响；HTTPS 部署仍需 `ADMIN_COOKIE_SECURE=true`。

## 部署检查要点

1. 后端保持默认 `SERVER_HOST=127.0.0.1`，由本机反向代理对内网提供 HTTPS。
2. 代理与后端同机时 `TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`；代理在别的主机时改为该代理主机的精确地址段。
3. 前端与 API 同源发布，`CORS_ALLOWED_ORIGINS` 留空。
4. 反向代理配置为覆盖/重写 `X-Forwarded-For`，并设置合理的超时（大于后端单次 SMTP 发送上限 10 秒）。
5. PostgreSQL 仅监听本机，数据库端口不对局域网开放。
