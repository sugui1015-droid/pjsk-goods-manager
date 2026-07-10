# 迁移准备状态

## 已完成

- 新版 `frontend/` 已建立为 Vue 3 + TypeScript 工程。
- 新版 `backend/` 已建立为 Go API 工程，并保留 `/health` 与 `/api/config`。
- 旧版 Streamlit 已移动到 `legacy-streamlit/`，迁移期间继续保留可运行。
- 本地 CSV、付款图片和真实 Excel 样本已有备份或测试副本。
- 数据库关系、Excel 解析规则、付款审核流程已整理到 `docs/`。

## 当前决定

- 新版用户鉴权先走 `CN + 查询码`，不做完整账号注册体系。
- 旧版 Streamlit 继续运行，直到新前后端完成对账、导入、审核三条主流程。

## 下一阶段

- 落 PostgreSQL migration。
- 接管理员登录和 JWT。
- 接基础订单查询与付款草稿。
- 表格导入在程序壳稳定后继续迁移。
