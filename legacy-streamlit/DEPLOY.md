# 部署到公网和 Supabase 云存储

推荐组合：

- 网页托管：Streamlit Community Cloud
- 数据云存储：Supabase Database + Supabase Storage

当前项目有两个入口：

- 管理员端：`main.py`
- 普通端：`user.py`

可以在 Streamlit Cloud 里建两个 app，分别选择这两个入口文件，这样会得到两个公网网址。

## 1. Supabase 初始化

在 Supabase 项目的 SQL Editor 里运行仓库里的 `supabase_schema.sql`。

它会创建：

- `records`：谷子明细数据
- `payment_records`：交肾截图记录
- `pjsk` Storage bucket：保存收款码和交肾截图

## 2. Streamlit Secrets

在 Streamlit Cloud 的 app 设置里添加 Secrets：

```toml
SUPABASE_URL = "你的 Supabase Project URL"
SUPABASE_SERVICE_ROLE_KEY = "你的 service_role key"
PJSK_SUPABASE_BUCKET = "pjsk"
```

建议用 `SUPABASE_SERVICE_ROLE_KEY`，并且只放在 Streamlit Secrets 里，不要写进代码或上传到 GitHub。

如果以后你想自定义表名，也可以加：

```toml
PJSK_RECORDS_TABLE = "records"
PJSK_PAYMENTS_TABLE = "payment_records"
```

## 3. 本地运行

如果本地没有配置 Supabase，程序会继续使用本地 CSV 和本地图片文件，不影响测试。

管理员端：

```bash
streamlit run main.py --server.port 8512
```

普通端：

```bash
streamlit run user.py --server.port 8513
```

## 4. 云端运行后的数据位置

配置 Supabase 后：

- Excel 导入后的明细保存到 Supabase `records`
- 交肾截图记录保存到 Supabase `payment_records`
- 管理员通过交肾记录后，普通用户不能再替换该条记录截图，但可以继续新增交肾截图记录
- 收款码保存到 Supabase Storage：`qr_codes/`
- 普通用户交肾截图保存到 Supabase Storage：`payment_images/`

这样管理员端和普通端会共享同一份数据。

## 5. 注意

- 不要把 Supabase key 写进 GitHub。
- `service_role key` 权限很高，只能放在 Streamlit Secrets 或服务器环境变量里。
- 如果 Supabase 没配置成功，页面会回退到本地模式，公网部署时就会出现数据不共享的问题。