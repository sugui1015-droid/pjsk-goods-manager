# 部署到公网和云储存建议

## 推荐方案

推荐先用：

- 网页托管：Streamlit Community Cloud
- 数据云储存：Supabase

这个组合最适合当前项目：Streamlit 负责生成公网网址，Supabase 负责长期保存表格数据、收款码、交肾截图。

## 为什么这样选

Streamlit Community Cloud 的优点：

- 免费
- 直接连接 GitHub 仓库
- 选择 repo、branch、入口文件后即可部署
- 每次 `git push` 后自动更新网页

Supabase 的优点：

- 有免费额度
- 同时支持数据库和文件储存
- 适合保存 Excel 拆出来的数据、交肾记录、二维码、付款截图

## 当前项目入口

当前本地有两个入口：

- 管理员端：`main.py`
- 普通端：`user.py`

部署时可以在 Streamlit Cloud 上建两个 app：

- 管理员 app 选择入口文件 `main.py`
- 普通用户 app 选择入口文件 `user.py`

这样会得到两个公网网址。

## 注意事项

当前代码仍然使用本地 CSV 和本地图片路径保存数据：

- `records.csv`
- `payment_records.csv`
- `payment_images/`
- `qr_codes/`

这在本机测试没问题，但部署到云端后不适合作为长期数据源。正式公网使用前，需要把这些保存逻辑改成 Supabase 数据库和 Supabase Storage。

## 建议的部署步骤

1. 注册 GitHub，把本项目上传到一个仓库。
2. 注册 Supabase，创建一个项目。
3. 在 Supabase 创建一个 Storage bucket，例如 `pjsk`.
4. 在 Streamlit Cloud 新建管理员 app，入口选择 `main.py`.
5. 在 Streamlit Cloud 新建普通用户 app，入口选择 `user.py`.
6. 在 Streamlit Cloud 的 Secrets 里保存 Supabase 的连接信息。
7. 把本地 CSV/图片保存逻辑替换为 Supabase 读写逻辑。

## 便宜服务器备选

如果不想改云储存逻辑，也可以用带持久磁盘的服务器直接跑 Streamlit。

更省事但需要一点费用：

- Render Web Service + Persistent Disk
- Railway
- 一台轻量 VPS

不过长期看，推荐还是 Streamlit Cloud + Supabase，因为后续维护更轻。
