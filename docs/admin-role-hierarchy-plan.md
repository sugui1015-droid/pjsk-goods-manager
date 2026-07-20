# 苏归与管理员分级权限系统——只读调查与设计方案(草稿)

状态:**方案草稿,未实施**。2026-07-21 起草,基于生产 `95036a0791` / 数据库 22/0022 的只读调查。等待人工确认后才进入开发;不修改代码、不提交、不推送、不动生产。

## 一、当前账号与权限模型(调查结论)

### 1. 两套完全独立的账号体系
| | 普通用户(客户) | 管理员 |
|---|---|---|
| 表 | `users`(0001) | `admins`(0001) |
| 标识 | `cn_code`(唯一,导入时创建) | `username`(唯一) |
| 凭据 | `query_code_hash`(查询码) | `password_hash`(bcrypt) |
| 登录入口 | `/api/query/login` | `/api/admin/login` |
| 会话表 | `query_sessions` | `admin_sessions`(含 `reauth_at`) |
| 角色 | 无角色字段 | `role in ('admin','owner')`(0022 加 CHECK) |

- **两表之间没有任何外键或关联字段**;同一个人要既是客户又是管理员,只能各自持有一个互不知晓的账号。
- **不存在管理员管理 API**:管理员只能通过服务器 CLI `create-admin` 创建,owner 只能通过 CLI `promote-owner` 产生(全库唯一)。路由中没有 `/api/admin/admins*`。
- 现有数据:`admin`(owner,active)、`testadmin`(admin,disabled),45+1 个用户。

### 2. 数据库层已有的 owner 保障(0022,可直接复用)
- `admins_role_check`:角色只允许 `admin`/`owner`;
- `admins_single_owner_unique` 部分唯一索引:**全库至多一个 owner**;
- `admins_protect_last_owner_trigger`(deferred constraint trigger):最后一个 active owner 不可删除/停用/降级,SQL 控制台也绕不过;单事务内转移 owner 仍可行。

### 3. 前端现状
- `frontend/src/App.vue` 单文件(约 5400 行),`isOwner = role === 'owner'` 控制 owner 专属界面;审计事件已有中文标签映射(如 `owner_promoted` → "升级为系统所有者")。
- 页面未发现直接渲染裸 `owner` 字符串的位置,但也**没有统一的角色显示名映射层**。

### 4. 对用户提出的模型问题的直接回答
| 问题 | 结论 |
|---|---|
| 用户与管理员是否同表 | 否,完全分表 |
| 同一账号可否兼具两种身份 | 目前不可 |
| `admins` 与 `users` 如何关联 | 无关联 |
| 能否直接把用户提升为管理员 | 不能(无关联、无 API) |
| 设为管理员后订单/付款是否保留 | 保留(业务数据只挂 `users.id`,不受影响) |
| 撤销管理员后能否继续作为用户登录 | 能(两套凭据互不影响) |
| 登录入口是否保持分离 | 建议保持分离(现状即分离,凭据体系不同,合并风险高收益低) |

## 二、与需求的差距

1. 无"从用户列表设为管理员"的能力(无关联字段、无 API、无 UI);
2. 无管理员列表/启停/撤销/重置的 API 与页面(只有 CLI);
3. 无角色显示名统一映射("苏归/管理员/用户");
4. 审计词表缺少管理员任免类事件;
5. 角色变更后的会话吊销逻辑不存在(仅改密/CLI 重置有类似逻辑可参考);
6. owner 保护在数据库层已完备,但"普通管理员不得操作 owner"需要在新 API 层强制。

## 三、推荐数据模型(迁移 0023,全部增量、可回滚)

```sql
-- 0023_admin_user_link_and_management.sql(草案)
alter table admins
    add column if not exists user_id uuid unique references users(id) on delete set null;
-- 一个用户至多关联一个管理员账号;删用户不删管理员,只断链

alter table admin_auth_audit_events
    drop constraint admin_auth_audit_events_event_type_check,
    add constraint admin_auth_audit_events_event_type_check check (event_type in (
        …原 16 项…,
        'admin_appointed',        -- 苏归任命管理员(含从用户设为管理员)
        'admin_revoked',          -- 苏归撤销管理员
        'admin_enabled',          -- 苏归启用管理员
        'admin_disabled',         -- 苏归停用管理员
        'admin_password_reset_by_owner'  -- 苏归重置管理员密码/触发安全重置
    ));
-- reason_code 词表增加 'target_is_owner'、'already_admin'、'user_not_found' 等
```

- **不改** `role` 的取值('owner' 技术值保持英文,显示层翻译);
- **不新建**独立角色表:两级角色 + CHECK 约束已足够,引入角色表是过度设计;
- 采用**账号关联**(admins.user_id)而非合并账号:保留两套凭据与入口,业务数据零迁移。

## 四、推荐接口(全部要求后端强制鉴权,前端隐藏按钮仅是体验)

统一前缀 `/api/admin/owner/admins`,全部 `RequireAuthentication + RequireOwner`;**变更类再加 `RequireRecentReauth`**(复用 10 分钟 reauth 门禁):

| 方法/路径 | 作用 | 门禁 |
|---|---|---|
| GET `/api/admin/owner/admins` | 管理员列表(姓名、账号、关联用户 CN、角色显示名、状态、创建时间、最近登录) | owner |
| POST `/api/admin/owner/admins` | 任命:`{user_id?, username, display_name?, initial_password}`;user_id 可选(支持无关联的纯管理员) | owner + reauth |
| POST `…/{id}/disable` / `…/{id}/enable` | 停用/启用 | owner + reauth |
| POST `…/{id}/revoke` | 撤销管理员(降为无管理员身份:删除 admins 行或置 disabled+revoked,推荐**软撤销**保审计外键) | owner + reauth |
| POST `…/{id}/reset-password` | 重置登录凭据(设新初始密码,吊销其全部会话) | owner + reauth |
| GET `/api/admin/owner/admins/{id}/audit` | 该管理员操作审计 | owner |

后端每个变更处理器强制:
1. 操作者 `role == 'owner'`(中间件 + 存储层双重校验);
2. 目标 `role != 'owner'`(禁止任何接口操作苏归账号;数据库触发器兜底);
3. 不提供任何"提升为 owner"的 HTTP 接口——owner 转移仍只走服务器 CLI(现状保留);
4. 变更成功后**删除目标的全部 `admin_sessions`**;
5. 每次变更写审计:操作人、目标人、时间、reason、结果(复用 `admin_auth_audit_events`)。

关于"是否允许管理员再设置管理员"的调查建议:**默认仅苏归可设**(与需求一致)。理由:当前审计词表、reauth 门禁、owner 保护都以 owner 为唯一高权主体,放开给 admin 需要再引入"授权层级不可越级"的复杂校验,收益低;QQ 群中"管理员不能设管理员"也符合直觉。若未来需要,可加配置开关再评估。

## 五、推荐页面流程(App.vue 内新增 owner 专属区)

1. **角色显示名统一映射**(一处定义,处处引用):`owner→苏归、admin→管理员、(用户端)→用户`;全站排查禁止裸 `owner` 字样;
2. **用户管理页**:owner 视角每行增加"设为管理员"按钮(已是管理员的显示"已是管理员/查看");点击弹确认框(显示 CN、显示名,输入登录用户名与初始密码)→ 触发 reauth → 成功提示;
3. **管理员列表页**(owner 专属):列 = 姓名、账号、对应用户 CN(可跳转)、角色(显示名)、状态、创建时间、最近登录;行操作 = 启用/停用、重置密码、撤销,全部先确认弹窗再 reauth;
4. admin 登录后看不到以上入口(且后端 403);
5. 被任命者首次登录后引导改密(复用现有改密页)。

## 六、安全与审计方案汇总

- 数据库层:单 owner 唯一索引 + 最后 owner 保护触发器(已有,0022);`admins.user_id unique` 防一人多管理员号;
- API 层:owner-only + reauth + 目标非 owner 三重校验;无任何 owner 提升 HTTP 面;
- 会话:任免/停用/重置即刻吊销目标全部会话;
- 审计:新增 5 个事件类型,记录 actor/target/ip/reason/result;
- 生产纪律:全部变更走正式 API,禁止 SQL 直改账号(与 2H-Final 门禁一致)。

## 七、测试方案

1. 单元:处理器鉴权矩阵(owner/admin/匿名 × 各端点)、目标为 owner 时全部 403、reauth 过期 401;
2. 集成(testdb):任命→登录→停用→会话失效→启用→撤销→用户端登录不受影响;单 owner 索引与触发器回归;审计行完整性;
3. 迁移:0023 在 0022 库上可重复执行(if not exists)、约束替换正确;
4. 云端验收:用既有测试用户 `production_write_test_20260721` 走"设为管理员→撤销"全链路(复用受控写入模式,留痕可接受)。

## 八、分阶段实施计划

| 阶段 | 内容 | 产出 |
|---|---|---|
| R1 | 迁移 0023 + 存储层 + API + 单元/集成测试 | 后端可用,CLI 兼容 |
| R2 | 前端角色显示名映射 + 用户页"设为管理员" + 管理员列表页 | UI 完整 |
| R3 | 构建、上传新 release、云端签收、迁移 0023、验收(测试 CN 链路) | 上线 |

每阶段独立可停;R3 前生产无任何变化。

## 九、对现有生产数据的影响与回滚

- 0023 仅**加列(可空)+ 换 CHECK 词表**,零数据改写;现有 admin/testadmin/users 不动;
- 回滚:应用层回滚 release 即可(旧代码不读新列;审计 CHECK 只放宽不收紧,旧事件全部合法);数据库无需回滚;
- 与"云端唯一事实源"纪律无冲突。

## 十、人工决策结果(2026-07-21 owner 已确认)

1. **软撤销**,不物理删除 admins 行;
2. 仅苏归可任命、撤销、启用、停用管理员;普通管理员不得任命管理员;
3. 显示名统一 owner→苏归、admin→管理员、客户→用户;数据库/后端技术值保持 `owner`/`admin` 不变;
4. 用户与管理员**账号关联**,不合并两套登录体系;管理员入口与用户入口保持分开;
5. 被任命后原用户身份、订单、付款、查询码全部保留;撤销后仍可作为普通用户登录;
6. **初始凭据改为系统生成的一次性临时密码**:密码学随机生成,明文只向苏归展示一次,目标管理员**首次登录强制改密**;不允许苏归设置长期固定密码(较原方案第四节的"苏归设初始密码"更严格,实施时需增加 `admins.must_change_password` 标记列与登录强制改密流程,已纳入迁移 0023 草案范围);
7. 任命、撤销、启用、停用、密码重置均要求苏归完成 10 分钟 reauth,并撤销目标管理员现有全部会话;
8. 不提供网页 owner 提升或苏归转移接口;苏归转移仍只允许 SSH CLI;
9. R3 验收复用 `production_write_test_20260721` 测试用户;
10. 本方案暂时只作为实施计划,不自动修改代码、数据库或生产环境;待 owner 审阅阶段十三日志与本方案后再开工。
