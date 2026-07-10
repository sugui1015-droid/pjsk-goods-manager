# PJSK Goods Manager

这是一个用 Streamlit 写的谷子管理系统，支持管理员端和普通查询端。

## 功能

- Excel 导入和重复记录跳过
- 按 CN、谷子名称、角色、是否已收、团购批次筛选
- 任意筛选组合的金额统计
- 按 CN 汇总和导出表格
- 系列累计金额
- 收款码设置
- 普通用户提交交肾截图，管理员查看交肾记录
- 管理员维护交肾状态
- 微信交肾金额展示手续费参考金额
- 配置 Supabase 后支持云端共享数据和图片
- 管理员通过交肾记录后，该记录截图会锁定，普通用户仍可新增截图记录

## 本地运行

管理员端：

```bash
streamlit run main.py --server.port 8512
```

普通端：

```bash
streamlit run user.py --server.port 8513
```

## Supabase

先在 Supabase SQL Editor 运行 `supabase_schema.sql`，再在 Streamlit Secrets 里配置：

```toml
SUPABASE_URL = "你的 Supabase Project URL"
SUPABASE_SERVICE_ROLE_KEY = "你的 service_role key"
PJSK_SUPABASE_BUCKET = "pjsk"
```

没有配置 Supabase 时，程序会继续使用本地数据文件，方便本地测试。