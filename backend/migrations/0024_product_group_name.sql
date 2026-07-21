-- 团名（group_name）与系列（series_code）分离。
--
-- 背景：旧解析逻辑把角色列头的前缀（例如 "25h miku" 的 "25h"）写进了
-- products.series_code，页面上叫「谷子系列」。按最终语义，那个前缀是**团名**，
-- 真正的系列来自模板 B2「分类(系列号)」那一行的值。
--
-- 本迁移只新增 group_name 字段，不改动 series_code / category 的既有数据，
-- 旧记录的 series_code 保持原样由读取层兼容处理。
alter table products
    add column if not exists group_name text;

create index if not exists products_group_name_idx on products (group_name);
