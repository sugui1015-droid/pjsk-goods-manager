# 备份状态

## 已完成

- 本地运行数据已备份到：
  - `D:\pjsk\backups\pre-migration-2026-07-10\localappdata-pjsk-goods-manager\records.csv`
  - `D:\pjsk\backups\pre-migration-2026-07-10\localappdata-pjsk-goods-manager\payment_records.csv`
  - `D:\pjsk\backups\pre-migration-2026-07-10\localappdata-pjsk-goods-manager\payment_images\`
- 真实 Excel 样本已复制到：
  - `D:\pjsk\testdata\excel\【生日羽排】汇总-20251001.xlsx`
  - `D:\pjsk\testdata\excel\26感谢祭单领.xlsx`

## 当前无法完成的部分

- PostgreSQL 备份：当前机器未发现正在使用的 PostgreSQL 连接信息。
- Supabase Storage 备份：当前机器未发现 `SUPABASE_URL` / `SUPABASE_SERVICE_ROLE_KEY` / `.streamlit/secrets.toml`。

## 结论

当前已完成“本地 CSV 与图片”备份。

云端 PostgreSQL / Storage 备份仍需你后续提供运行中的连接凭据，或在部署环境中执行导出。
