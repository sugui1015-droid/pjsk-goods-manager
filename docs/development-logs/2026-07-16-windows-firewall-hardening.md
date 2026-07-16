# 2026-07-16 Windows Defender 防火墙只读调查与安全启用方案设计(未实施)

> 安全边界:本文不记录任何密钥、密码、令牌或连接串。本轮**只读调查**,未开启防火墙、未新增/修改/删除任何规则、未修改网络类别、未启停服务、未修改注册表、未修改 PostgreSQL/Caddy/后端/数据库,未提交、未推送。
> 执行模式:Claude 会话无管理员权限;`Get-NetFirewallRule`(含 ActiveStore 与 PersistentStore)在本机需管理员,故规则枚举改用非特权可用的 `netsh advfirewall firewall show rule name=all verbose` 完整导出解析(745 条规则,含 Rule source 字段可区分本地与 GPO)。profile 级字段仍通过 `Get-NetFirewallProfile`(ActiveStore + PersistentStore)取得。未使用子代理。

## 1. 基线核对(全部通过)

- Git:`main`,HEAD 与 `origin/main` 均为 `f28325b7e6e93b8a3fa7d7b8c86463ab4177173b`,工作区与暂存区干净(本日志为本轮唯一新增文件)。
- 身份:`SUGUI\苏归`,非管理员会话。
- 服务:`pjsk-backend`、`pjsk-caddy`、`postgresql-x64-18` 均 Running / Auto;`mpssvc`(Windows Defender Firewall 服务)Running / Auto / `NT Authority\LocalService`(宿主 `svchost.exe -k LocalServiceNoNetworkFirewall`)。
- 健康检查:后端直连 `http://127.0.0.1:8080/health`、Caddy 首页 `http://127.0.0.1:8081/`、Caddy 代理 `http://127.0.0.1:8081/health` 均 HTTP 200。
- PostgreSQL 仅监听 `127.0.0.1:5432` 与 `[::1]:5432`(PID 720);后端仅 `127.0.0.1:8080`;Caddy 双栈通配 `[::]:8081`(PID 4160)。

## 2. 当前网络环境(与既有记录不一致,关键发现)

- 唯一活动连接配置文件:`REDMI K80 57`(WLAN,InterfaceIndex 16),**NetworkCategory = Public**,IPv4/IPv6 Connectivity 均 Internet。
- IPv4:`192.168.16.93/24`,网关/DNS `192.168.16.119`(手机热点形态)。
- IPv6:**存在全局单播地址** `2409:895a:3976:7685::/64` 前缀下两个地址 + 链路本地地址。
- 与历史部署记录(2026-07-16 Caddy 部署日志中的 `192.168.1.10`、Private 假设)**不一致**:现网是 Public 类别的手机热点。按停止条件,本轮继续只读调查、不实施。
- 直接后果:现有 `PJSK Caddy HTTP LAN` 规则限定 Private profile——**在当前 Public 网络下,一旦开启防火墙,局域网访问 8081 会被默认阻止**。启用前必须先决定:在可信局域网上把网络类别改为 Private,或(不推荐)为 Public 增加 LocalSubnet 限定规则。
- IPv6 暴露面:防火墙三 profile 均关闭 + Caddy 监听 `[::]` + 本机持有全局 IPv6 地址 ⇒ 8081 目前理论上可从公网 IPv6 直达(仅受上游运营商/热点过滤影响)。**开启防火墙本身就是对该暴露面的直接缓解。**

## 3. 防火墙 profile 状态(ActiveStore 与 PersistentStore)

- 三个 profile(Domain/Private/Public)在 ActiveStore 中均:`Enabled=False`,`DefaultInboundAction=Block`,`DefaultOutboundAction=Allow`,`AllowInboundRules/AllowLocalFirewallRules/AllowLocalIPsecRules=True`,`NotifyOnListen=True`,`LogAllowed=False`,`LogBlocked=False`,日志路径 `%systemroot%\system32\LogFiles\Firewall\pfirewall.log`,`LogMaxSizeKilobytes=4096`。
- PersistentStore 中相应字段多为 `NotConfigured`(即未显式本地配置,ActiveStore 值来自平台默认),`Enabled=False` 一致。
- **无 GPO 干预**:745 条规则的 Rule source 全部为 `Local Setting`,0 条来自组策略;profile 值也无 GPO 覆盖迹象。ActiveStore 与 PersistentStore 无实质差异。

## 4. Caddy 真实入站入口

- 服务:`pjsk-caddy` 由 NSSM(`D:\PJSK-Service\backend\nssm.exe`)托管,运行账户 `NT Authority\LocalService`;真实程序 `D:\PJSK-Service\caddy\caddy.exe`,参数 `run --config D:\PJSK-Service\caddy\Caddyfile --adapter caddyfile`,工作目录 `D:\PJSK-Service\caddy`。
- 正式 Caddyfile 位于仓库外且目录 ACL 已收紧,非管理员不可读(符合既定安全设计);本轮依据仓库模板 `deploy/windows-service/Caddyfile.http-lan.example` 与部署日志既有记录交叉核对:`admin off`、`auto_https off`、单站点 `:8081`、反代 `127.0.0.1:8080` 的 `/api/*` 与 `/health`、其余为静态前端 + SPA 回退。
- 端口交叉验证:`Get-NetTCPConnection` 显示唯一 Caddy 监听为 `[::]:8081`(双栈通配,IPv4 经 dual-stack socket 同样可达,历史验收已证实 IPv4 可访问)。**无 80/443/2019(admin) 监听属于 Caddy**;80 由 http.sys/IIS 持有(PID 4),与 Caddy 无关。无自动 HTTPS、无 HTTP→HTTPS 跳转、无 ACME 端口需求。
- 结论:**唯一需要局域网入站的 Caddy 端口是 TCP 8081**(IPv4+IPv6);8080(后端)与 5432(PostgreSQL)均仅回环,依靠本机回环通信,不需要任何入站放行。
- 局域网访问 URL(当前网络下):`http://192.168.16.93:8081/`;IP 随热点 DHCP 可变,正式方案应以届时实际地址为准。

## 5. 现有规则结论(netsh 全量导出 745 条,373 条启用的入站允许)

- **`PJSK Caddy HTTP LAN` 存在且启用**:Inbound / Allow / TCP 8081 / Profile=**Private** / RemoteIP=**LocalSubnet** / Program=`D:\PJSK-Service\caddy\caddy.exe` / Edge=No / Security=NotRequired / 本地规则。字段与历史部署记录完全一致,无重复规则,范围最小化良好。唯一问题是 Private 限定与当前 Public 网络不匹配(见 §2)。
- **无任何允许 5432 或 8080 入站的规则**(启用或禁用状态均无端口命中)。
- 启用状态下涉及 80/443 的入站允许规则:
  - `万维网服务(HTTP 流量入站)` TCP 80 / 全 profile / 远程 Any / System(IIS);
  - `万维网服务(HTTPS 流量入站)` TCP 443 / 全 profile / 远程 Any / System(IIS);
  - `Siemens SCP Remote Communication` TCP 443 / 全 profile / 远程 Any(无程序限定)。
  开启防火墙后 IIS 80/443 将对任意远程地址开放——是否需要局域网访问 IIS(Siemens 工具链)**需人工确认**;若不需要,建议收窄为 LocalSubnet 或禁用(注意 Siemens/TIA 依赖,不建议贸然删除)。
- 危险内置组:远程桌面组未启用(3389 无启用规则);SMB 445 入站规则未启用;但 `文件和打印机共享(NB-Name-In)` UDP 137 在三个 profile 均启用且远程 Any;`网络发现` 组 13 条在 Private+Public 启用(多为 LocalSubnet);mDNS 3 条、"播放到设备"13 条、远程协助 6 条、AllJoyn/SSDP/WSD 等消费类功能组启用。
- 第三方程序 Any 入站规则(启用):**ToDesk 3 条、AweSun(向日葵类远控)6 条**、Rockwell/FactoryTalk 全家桶约 80 条、Siemens SIMATIC/UMC、GX Works2/MMSserve、Thunder/迅雷系(含多个 Temp 目录路径)、百度网盘、WPS、微信/企业微信/QQ、Steam、SafeNet/CodeMeter 加密狗服务、iNode、kingview/flexem 组态软件、`C:\windows\system32\ftp.exe` 等。GPO 规则 0 条。

## 6. 其他入站依赖分类(仅调查,未处置)

- **PJSK 必需**:caddy.exe TCP 8081(局域网入站);postgres 5432 与后端 8080 仅回环,无需入站。
- **Windows 基础**:核心网络组(DHCP/ICMPv6/IGMP 等,保留);svchost UDP 123(NTP)、500/4500(IKE)、1900/3702(SSDP/WSD)、5353/5355(mDNS/LLMNR)、5050、5040;lsass/wininit/services/spoolsv/svchost 的 RPC 高位端口(135 + 49xxx);System 137/138/139/445(SMB/NetBIOS,445 无启用入站规则)。
- **用户软件(可能需要局域网/远程)——需人工确认**:ToDesk(35600/37600 回环 + Any 规则)、AweSun、迅雷、百度网盘、WPS(大量非回环 UDP)、微信/QQ/企业微信、Steam、Chrome mDNS。
- **工业/工程软件——需人工确认是否需局域网入站**:s7oiehsx64(TCP 102,S7 通信)、HistorySvr 777、mqsvc(MSMQ 1801/2103/2105/2107)、spnsrvnt/sntlkeyssrvr(SafeNet 6001/6002/7001/7002)、MMSserve 9064/9063、KvHistDataSvr/KvSignatureSvr(41130/41630)、iNodeImg 8900/20102、mvserver 55355、CodeMeter 22350(回环)。
- **不明来源**:`D:\新建文件夹 (2)\Tencent Files\...\6eba0dadff35987a56dce0cd22f87acd.exe` 的 2 条 Any 入站规则(QQ 传输组件形态但路径可疑)——需人工确认。
- 本轮未关闭任何端口、未终止任何进程、未卸载任何软件。

## 7. 最小放行方案(仅清单,未执行)

原则:三 profile 全部 Enabled=True,默认入站 Block、出站 Allow;不关闭通知与日志(反而开启 LogBlocked);不使用任意程序/任意端口/任意远程地址的宽规则;IPv4 与 IPv6 均由同一规则的 LocalSubnet 语义覆盖(LocalSubnet 同时匹配 IPv4 子网与 IPv6 本地前缀,不遗漏 IPv6)。

| 动作 | 规则 | 说明 |
|---|---|---|
| 前置决策 | 网络类别 | 在可信局域网上将该网络设为 Private(用户人工确认网络可信后另行操作);**不建议**为 Public 开 8081 |
| 保留(收窄确认) | `PJSK Caddy HTTP LAN` | 现状已是最小化(TCP 8081/Private/LocalSubnet/绑定 caddy.exe),保留;不新建重复规则 |
| 不新增 | 5432 / 8080 | 明确不建立任何局域网允许规则 |
| 人工确认后决定 | IIS 80/443 两条 + Siemens SCP 443 | 若无局域网访问需求,收窄为 LocalSubnet 或禁用(勿删,涉及 Siemens/IIS 依赖) |
| 建议收窄/禁用 | `文件和打印机共享(NB-Name-In)` UDP137 远程 Any×3 | 若不需要共享,禁用;需要则收窄 LocalSubnet+Private |
| 人工确认 | ToDesk / AweSun / 迅雷 / 网盘 / 不明 exe 的 Any 入站规则 | 远控软件是否保留由用户决定;不明路径 exe 规则建议禁用前先人工核实 |
| 保留 | 核心网络组、出站默认 Allow | 保证正常联网 |
| 开启 | 三 profile `LogBlocked=True` | 日志路径沿用默认,便于启用后排障 |

## 8. 实施与回滚脚本设计(仅设计,未创建可执行实施)

计划脚本:`D:\PJSK-Archive\firewall\Set-PjskFirewallBaseline.ps1`(仓库外)。要求:默认 Plan 模式 / 显式 `-Apply` / `-Rollback`;管理员门禁;门禁校验当前网络类别、接口、网段与批准时调查结果一致(不一致即拒绝);校验 caddy.exe 路径、8081 监听、三服务 Running;`-Apply` 前完整导出现有规则(`netsh advfirewall export` + wfw 文件)与三 profile 原状态到 `D:\PJSK-Archive\firewall\backup-<timestamp>\`;每个写操作后立即读回复核;任一步失败自动按已记录原状态回滚;日志存仓库外、不记录秘密;幂等(已处于目标状态时安全退出 0);不依赖交互式多行粘贴。
回滚设计(优先精确恢复,不以"关闭全部防火墙"为唯一手段):恢复三 profile 原 Enabled/默认策略 → 删除本次新增的唯一内部名称规则(如有)→ 恢复本轮被禁用/收窄规则的原状态(依据备份快照)→ 验证三服务 Running/Auto、本机三项 health 200、8081 局域网入口恢复至变更前状态。

## 9. 验收矩阵(未来实施时)

本机:三服务 Running/Auto;后端直连 health 200;Caddy 首页/代理 health 200;PostgreSQL 仍仅 `127.0.0.1`+`::1`;后端仍仅回环;`[::]:8081` 仍监听;三 profile Enabled 且默认 Block/Allow 符合批准方案;防火墙日志文件可写并出现记录;`PJSK Caddy HTTP LAN` 唯一、启用、字段逐项一致;确认无 5432/8080 局域网允许规则。
局域网第二设备(真实设备,不得用回环冒充):通过 `http://<服务器IP>:8081/` 打开页面、资源加载正常、登录与查询功能正常;直连 5432 失败;直连 8080 失败;非批准端口(如 102/777/9064 等,视人工确认结果)不可达。当前热点网络曾有设备互访能力(历史 §16),届时如换网需先确认无 AP 隔离。**本轮未做任何第二设备验证,全部标记为"待真实第二设备验证"。**

## 10. 待用户确认清单

1. 何时/是否将当前(或未来部署所在)网络类别改为 Private;
2. IIS 80/443 与 Siemens SCP 443 是否需要局域网入站;
3. ToDesk、AweSun 两个远控是否保留 Any 入站;
4. 工业软件(S7 102、MSMQ、SafeNet、Kv*、MMSserve、iNode、mvserver)是否需要局域网入站;
5. `D:\新建文件夹 (2)\Tencent Files\...` 下不明 exe 的入站规则处置;
6. 文件/打印机共享与网络发现是否需要;
7. 批准后方可编写并执行 `Set-PjskFirewallBaseline.ps1`。

## 11. 实施脚本设计与 Plan 验证(第二轮,未实施)

- 新建仓库外脚本 `D:\PJSK-Archive\firewall\Set-PjskFirewallBaseline.ps1`(PowerShell 5.1,PSParser 0 错误),SHA-256 `C1005E8131C785EA9CC63E8C801F55BAF7BE4768A7F30CDD2CAC3752ECE03602`。
- 模式:默认 Plan(只读)/ `-Apply`(第一阶段基线)/ `-Rollback -BackupPath <绝对路径>` / `-SelfTest`(非侵入自测)。退出码约定已写入脚本注释与 Plan 输出:0=就绪、2=Plan 完成但门禁未满足、1=脚本错误/失败。
- 第一阶段 `-Apply` 范围仅:三 profile 启用 + In=Block/Out=Allow + NotifyOnListen=True + LogBlocked=True;日志路径与 4096 KB 上限保持默认;复用现有 `PJSK Caddy HTTP LAN`;不为 5432/8080 建规则。**脚本内不含任何规则增删改停用 cmdlet,也不含修改网络类别的 cmdlet(自测 T23/T24 静态证明)。**
- 门禁(`Test-ApplyGate` 纯函数):管理员;mpssvc Running/Auto;活动网络恰 1 个且必须 Private(Public/DomainAuthenticated 硬拒,输出固定 NOT READY 文案);不按网络名称判信、不自动改类别;`-Apply` 强制要求 `-PlanSnapshotPath`(由批准的 Plan 用 `-SnapshotPath` 生成),接口/类别/IPv4/网关任一漂移即拒绝;Caddy 程序路径精确匹配、服务 Running/Auto、8081 监听;8080 与 5432 仅回环;三项 health 200;PJSK 规则存在唯一且八字段逐项匹配;无 5432/8080 入站允许规则。规则核验经 `netsh advfirewall` 解析(本机 NetSecurity 规则 cmdlet 读取需管理员,netsh 只读非特权可用)。
- 备份:`backup-<时间戳>.partial` 内生成 `netsh advfirewall export` 的 .wfw、profiles/rules/network/services-listeners/health 五个 JSON、标记文件与 SHA-256 清单,清单自验通过后原子改名发布并复验;不记录任何秘密。
- 写序:全部门禁→备份→逐 profile 设默认策略并读回→规则复核→启用 Private→本机健康+8081 核验→启用 Public→再核验→启用 Domain→终态全量验证;任一步失败自动按备份精确恢复(恢复原 Enabled/默认策略/通知/日志设置;因变更前三 profile 均关闭,忠实回滚会恢复为关闭——这是恢复原状的结果,脚本未硬编码"关闭防火墙"为通用回滚)。
- `-Rollback` 路径校验:拒绝空/相对/仓库目录外/`.partial`/缺标记文件/清单哈希不符;回滚后验证三服务、三 health、5432/8080 仅回环、三 profile 与备份一致、PJSK 规则唯一。
- 自测:24/24 通过(非管理员拒绝、Public/DomainAuthenticated 拒绝、Private 通过、多网络/Caddy 路径/8081/非回环 8080/非回环 5432/规则缺失/重复/错端口/含 Public/RemoteIP Any/存在 5432 或 8080 允许规则各自拒绝、无效与 .partial 回滚路径拒绝、哈希篡改拒绝、Plan 无写操作、真实机器判定 NOT READY、finally 清理临时目录、无改网络类别代码、无规则 cmdlet),全程仅用模拟状态与临时文件,未触碰真实防火墙。修复一处集合返回包装缺陷(`,$list` 经 `@()` 收集不展开导致空列表计 1)。
- 真实 Plan 运行(非管理员,只读):完整输出身份、网络(REDMI K80 57 / WLAN idx16 / **Public** / 192.168.16.93 / 网关 192.168.16.119)、三 profile 状态、mpssvc、Caddy/8080/5432 监听与规则计数、health 200×3、拟改字段、备份与回滚说明;门禁失败两条(非管理员;`NOT READY FOR APPLY: active network profile is Public; …`),**退出码 2**。Public 网络下判定未就绪是预期正确结果,非脚本失败。
- 本轮未执行 `-Apply`/`-Rollback`;未修改网络类别、防火墙、任何规则、服务、Caddy、PostgreSQL、后端、数据库、注册表;无残留临时/.partial 目录;未使用子代理;未提交未推送。
- 下一次正式实施前提:用户确认部署网络可信并**人为**将其设为 Private,在该网络上以管理员运行 Plan(带 `-SnapshotPath`)取得退出码 0,人工批准后方可 `-Apply -PlanSnapshotPath <快照>`。

## 12. 规则 count=0 矛盾调查与采集层修复(第三轮,只读+仓库外脚本修复)

- 背景:网络设为 Private(CMCC-4cXx / WLAN idx16 / 192.168.1.10 / 网关 192.168.1.1)后,管理员窗口 Plan 退出码 2,唯一失败 `Rule 'PJSK Caddy HTTP LAN' is missing.`,与既有记录矛盾。
- **判定:情况 A——规则真实存在,系脚本采集缺陷,规则从未被删除。**证据链:
  1. 原始 `netsh advfirewall firewall show rule name="PJSK Caddy HTTP LAN" verbose`(本轮实测):规则存在,Enabled/In/Allow/Private/LocalSubnet/TCP 8081/`D:\PJSK-Service\caddy\caddy.exe`/Local Setting,逐字段与历史一致;全量 netsh 输出中该规则块完整无异常。
  2. 语言无关 COM(`HNetCfg.FwPolicy2`,非管理员可读):745 条规则中命中恰 1 条,数值字段 Direction=1(In)、Action=1(Allow)、Protocol=6(TCP)、LocalPorts=8081、RemoteAddresses=LocalSubnet、Profiles=2(Private)、App=caddy.exe 路径精确。
  3. 防火墙事件日志(Microsoft-Windows-Windows Firewall With Advanced Security/Firewall,事件 2004/2005/2006 等)自 16:00 以来**无任何规则新增/修改/删除事件**;`Set-NetConnectionProfile` 仅改网络类别,无删规则的证据。
  4. NetSecurity cmdlet 在本工具会话(非管理员)仍为 Access denied,双存储权威查询命令已交管理员窗口执行留档(预期与上述一致)。
- 根因定性:旧版脚本仅依赖 `netsh ... show rule name=all verbose` 的**英文字段名文本解析**(锚点 `^Rule Name:`)。该输出的字段标签与形态随控制台语言/环境而变,在管理员窗口环境下解析得 0 条,被误判为"规则缺失"。文本解析天然脆弱,属采集层缺陷,非规则状态变化。
- 修复(仅仓库外脚本):新增 `Get-FirewallRuleFacts` 三层采集——① 管理员优先 NetSecurity 对象 + 关联过滤器(权威,含 `Get-NetFirewallPortFilter` 全量索引反查 5432/8080);② `HNetCfg.FwPolicy2` COM(语言无关数值字段,非管理员可用,新增 `ConvertFrom-ComFirewallRule` / `ConvertFrom-NetSecurityFirewallRule` 归一化);③ netsh 文本仅作最后回退,解析得 0 条时标记 `Reliable=false`。门禁新增:规则事实不可靠时输出 "Firewall rule state could not be reliably verified" 并拒绝就绪(退出码 2),**绝不把不可解析当作规则缺失或就绪**。Plan 输出增加 rule facts source/reliable 字段。
- 自测扩至 **27/27 通过**:新增 T25(本机真实英文 netsh 原始输出回归解析 count=1 且字段正确)、T26(不可靠采集被拒且不误报 missing)、T27(COM 实测值归一化后通过门禁);T21 改为真实机器回归断言(采集可靠且 count=1,本次实测 source=COM);缺失/重复/错端口/错程序/Any/Public 等负向测试全部保持通过。
- 本轮真实 Plan(非管理员,只读):rule facts source=COM、reliable=True、count=1,5432/8080 允许规则均 0,网络 Private,唯一门禁失败为本会话非管理员,退出码 2。**旧快照 `plan-snapshot-20260716-175448.json` 对应旧脚本版本,不得用于 Apply;须以管理员重跑 Plan 生成新快照。**
- 修复后脚本 SHA-256:`F4A91C711CA5BA75966B3FB86F155AFBBF7B3E6799298B77FF9AA40522F6D057`。
- 本轮未执行 `-Apply`/`-Rollback`;未创建/修改/启用/禁用/删除任何防火墙规则;未修改 profile、网络类别、服务、Caddy、PostgreSQL、后端、数据库、注册表;无残留临时目录;未使用子代理;未提交未推送。

## 13. 正式实施、热点访问恢复与最终验收(已完成)

- 正式实施(管理员窗口,人工批准后执行):`Set-PjskFirewallBaseline.ps1 -Apply -PlanSnapshotPath D:\PJSK-Archive\firewall\plan-snapshot-20260716-181635.json`,**APPLY_EXIT_CODE=0**。脚本 SHA-256 保持 `F4A91C711CA5BA75966B3FB86F155AFBBF7B3E6799298B77FF9AA40522F6D057`。
- 正式备份(实施前自动创建并哈希验证):`D:\PJSK-Archive\firewall\backup-20260716-181833`(netsh 导出 .wfw + profiles/rules/network/services-listeners/health JSON + 清单),仓库外保留,回滚入口 `-Rollback -BackupPath <该目录>`。
- 终态验收(全部通过):
  - Domain、Private、Public 三个 profile 均 **Enabled**;默认入站 **Block**、默认出站 **Allow**;NotifyOnListen=True;LogBlocked=True;日志路径与 4096 KB 上限保持 Windows 默认;
  - `PJSK Caddy HTTP LAN` 规则保持启用,未新建、未修改任何规则;未新增 TCP 8080 或 5432 入站允许规则;
  - PostgreSQL 5432 仍仅监听 `127.0.0.1`+`::1`;后端 8080 仍仅 `127.0.0.1`;Caddy 8081 正常监听;
  - 三项服务均 Running / Automatic;后端直连 health、Caddy 首页、Caddy 代理 health 均 HTTP 200。
- 跨设备验收:手机通过电脑移动热点访问 PJSK **已恢复并通过**。`CMCC-4cXx` 路由器存在客户端/AP 隔离,同一路由器下设备不能互访——这是网络侧既有条件,非防火墙或 PJSK 故障,保留为独立网络限制。
- 影响观察:普通软件下载正常,未发生防火墙导致的下载失败。工程软件未来若出现设备搜索、许可证或局域网通信问题,应单独检查对应软件的具体规则,**不得预先关闭整个防火墙**。
- 遗留独立待办(本轮未处理、未修改):IIS 80/443、Siemens SCP 443、ToDesk、AweSun、Rockwell/GX Works/许可证服务等第三方宽规则的审查收窄;文件/打印机共享与网络发现规则取舍;不明路径 EXE 规则核实。
- 至此"Windows 防火墙三个 profile 全部关闭"这一独立遗留风险**已解决**。

## 14. 本轮边界声明

未修改任何系统状态(防火墙、规则、网络类别、服务、注册表、Caddy、PostgreSQL、后端、数据库均未动);未提交、未推送;未使用子代理;唯一文件变更为本日志。

## 15. 防火墙启用后真实整机重启验收 —— 重启前基线(2026-07-16 18:44)

> 本章为防火墙第一阶段基线正式实施(§13)之后的整机重启自动恢复验收。只读采集,未做任何修改。

- 采集时间:2026-07-16 18:44:19 +08:00;**重启前 Windows 启动时间:2026-07-16 14:31:25**(重启后启动时间必须晚于该值)。
- Git:`main`,HEAD = origin/main = `815e30fe63f51ae81db3755bee72fa411eeb9944`,工作区与暂存区干净(除本日志追加)。
- 服务:`postgresql-x64-18`、`pjsk-backend`、`pjsk-caddy`、`mpssvc` 均 Running / Auto。
- 防火墙:Domain/Private/Public 均 Enabled、In=Block、Out=Allow、NotifyOnListen=True、LogBlocked=True。
- 网络:`CMCC-4cXx` / WLAN idx16 / **Private** / 192.168.1.10(该路由器存在客户端/AP 隔离,同路由器手机访问失败不判为 PJSK 故障)。
- 监听:5432 仅 `127.0.0.1`+`::1`(PID 720);8080 仅 `127.0.0.1`(PID 7408);8081 为 `[::]`(PID 4160)。
- HTTP:后端直连 health、Caddy 首页、Caddy 代理 health 均 200。
- PostgreSQL READ ONLY 验证(pgpass,显式只读事务,退出码 0):`listen_addresses='127.0.0.1, ::1'`、`context=postmaster`、`pending_restart=false`、`pg_file_settings` 目标行 `applied=true`、error 计数 0、迁移 19 条、最高 `0019_admin_auth_audit_events.sql`、`0019` 恰 1 条。
- 备份存在性:防火墙备份 `D:\PJSK-Archive\firewall\backup-20260716-181833` 存在;`D:\PJSK-Archive\postgres` 下 PostgreSQL 配置备份 5 份存在。
- 重启由用户人工执行 `Restart-Computer`;本会话不执行任何重启操作。

### 重启后继续指令(中断恢复用,粘贴给 Codex)

"继续执行 PJSK 真实整机重启后的自动恢复验收。不得修改配置,先确认 Windows 启动时间已经晚于重启前记录(2026-07-16 14:31:25),然后完成以下只读验证:服务自动启动(postgresql-x64-18 / pjsk-backend / pjsk-caddy / mpssvc 均 Running/Automatic,不得手动启动)、启动顺序事件、监听(5432 仅回环双栈、8080 仅 127.0.0.1、8081 正常)、防火墙三 profile 与 PJSK Caddy HTTP LAN 规则、三项 HTTP 200 与页面验证、数据库 READ ONLY 完整性(listen/context/pending/applied/19/0019/1)、后端 ::1→::1:5432 连接、网络访问记录;全部通过后追加日志、更新 HANDOVER.md,提交标题 docs: record full reboot validation 并普通推送。"

## 16. 防火墙启用后真实整机重启验收 —— 重启后结果(全部通过,2026-07-16 18:51)

- **真实整机重启确认**:重启后 Windows 启动时间 **2026-07-16 18:47:03**(晚于重启前 14:31:25),System 日志内核启动事件(Kernel-General Id=12,18:47:03)佐证,非注销/睡眠/仅重启服务。Git 工作区无重启导致的未知变化(唯一修改为重启前追加的本日志)。
- **服务自动启动(未做任何手动启动)**:`postgresql-x64-18`、`pjsk-backend`、`pjsk-caddy`、`mpssvc` 均 Running / Auto。
- **启动顺序**(Win32_Process 创建时间):postgres 主进程 18:47:18.7 → pjsk-backend 18:47:20.6 → caddy 18:47:30.7,与依赖链一致;各服务单一稳定 PID,自启动以来 SCM 无 7031/7034/7024 崩溃事件,PJSK/PostgreSQL 无任何服务失败事件,无 CrashLoop。
- **监听**:5432 仅 `127.0.0.1`+`::1`(PID 9108);8080 仅 `127.0.0.1`(PID 7712);8081 `[::]`(PID 14560);PostgreSQL 无 `0.0.0.0`/`::` 监听,后端无非回环监听。
- **防火墙**:三 profile 均 Enabled、In=Block、Out=Allow、NotifyOnListen=True、LogBlocked=True;`PJSK Caddy HTTP LAN` 唯一、启用、字段逐项正确(COM 数值核验:In/Allow/TCP/8081/LocalSubnet/Private/caddy.exe);无 5432/8080 入站允许规则;全程未关闭防火墙。
- **HTTP 与页面**:后端直连 health、Caddy 首页、Caddy 代理 health 均 200;浏览器实测首页、`/query`(CN 查询页)、`/admin/orders`(SPA 回退至管理员登录页)均正常渲染,静态资源正常,控制台无红色错误;未登录、未新增用户、未录入付款、未改业务数据。
- **数据库 READ ONLY 完整性**(pgpass,显式只读事务 `transaction_read_only=on`,退出码 0):`listen_addresses='127.0.0.1, ::1'`、`context=postmaster`、`pending_restart=false`、目标行 `applied=true`、error 计数 0、迁移 19 条、最高 `0019_admin_auth_audit_events.sql`、`0019` 恰 1 条;未读业务表。
- **后端真实数据库连接**:OS TCP 连接表与 PID 交叉验证,`::1 → ::1:5432`(PID 7712),未读取 `.env` 或连接串。
- **网络访问**:当前连接 `CMCC-4cXx`(Private),该路由器存在客户端/AP 隔离,同路由器手机访问失败不判为 PJSK 故障;本轮未重做手机热点测试——**本机重启验收通过,跨设备验收沿用此前已通过证据**(§13 与 Caddy 部署日志 §16)。
- 边界:未手动启动服务;未修改任何配置、服务、防火墙、规则、路由器或数据库;未使用子代理。
