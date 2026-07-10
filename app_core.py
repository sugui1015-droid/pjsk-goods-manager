from io import BytesIO
import hashlib
import os
from pathlib import Path
from datetime import datetime
from decimal import Decimal, ROUND_CEILING
from typing import Dict, Iterable, List, Optional, Tuple

import pandas as pd
import streamlit as st

try:
    from supabase import create_client
except ImportError:
    create_client = None


BASE_DIR = Path(__file__).resolve().parent
DATA_DIR = Path(os.environ.get("PJSK_DATA_DIR") or os.environ.get("LOCALAPPDATA") or str(Path.home())) / "pjsk-goods-manager"
DATA_PATH = DATA_DIR / "records.csv"
PAYMENT_PATH = DATA_DIR / "payment_records.csv"
PAYMENT_IMAGE_DIR = DATA_DIR / "payment_images"
QR_DIR = DATA_DIR / "qr_codes"
ALIPAY_QR_PATH = QR_DIR / "alipay.png"
WECHAT_QR_PATH = QR_DIR / "wechat.png"
SUPABASE_RECORDS_TABLE = "records"
SUPABASE_PAYMENTS_TABLE = "payment_records"
SUPABASE_BUCKET = "pjsk"
SUPABASE_PREFIX = "supabase://"

PAYMENT_COLUMNS = [
    "payment_id",
    "cn",
    "item_list",
    "amount",
    "method",
    "note",
    "image_path",
    "approved",
    "approved_at",
    "created_at",
    "updated_at",
]

REQUIRED_COLUMNS = [
    "record_id",
    "cn",
    "item_name",
    "role",
    "batch",
    "quantity",
    "unit_price",
    "amount",
    "collected",
    "source_sheet",
    "source_file",
]

COLUMN_ALIASES = {
    "cn": ["cn", "cn名", "cn名称", "买家", "姓名", "昵称", "群昵称", "人员", "拍下人", "下单人", "收款对象"],
    "item_name": ["谷子名称", "谷名", "商品名称", "商品", "周边", "物品", "品名", "款式", "柄图", "名称"],
    "role": ["角色", "人物", "角色名", "推", "担当", "成员", "种类"],
    "batch": ["团购批次", "批次", "团", "团名", "车", "车次", "链接批次"],
    "quantity": ["数量", "件数", "个数", "qty", "q'ty", "份数"],
    "unit_price": ["单价", "价格", "单个价格", "均价", "price"],
    "amount": ["金额", "总金额", "合计", "小计", "应收", "应收金额", "实付", "总价", "尾款"],
    "collected": ["是否已收", "已收", "收款状态", "状态", "是否收款", "已收款", "收齐", "交肾状态"],
}

DISPLAY_NAMES = {
    "record_id": "记录编号",
    "cn": "CN",
    "item_name": "谷子名称",
    "role": "角色",
    "batch": "团购批次",
    "quantity": "数量",
    "unit_price": "单价",
    "amount": "金额",
    "collected": "是否已收",
    "source_sheet": "来源表",
    "source_file": "来源文件",
}


def normalize_name(value: object) -> str:
    return str(value).strip().lower().replace(" ", "").replace("_", "").replace("-", "").replace("/", "")


def clean_text(value: object) -> str:
    if pd.isna(value):
        return ""
    text = str(value).strip()
    if text.lower() == "nan":
        return ""
    return text


def find_column(columns: Iterable[object], candidates: List[str]) -> Optional[object]:
    normalized = {normalize_name(column): column for column in columns}
    for candidate in candidates:
        key = normalize_name(candidate)
        if key in normalized:
            return normalized[key]
    for normalized_column, original_column in normalized.items():
        if any(normalize_name(candidate) in normalized_column for candidate in candidates):
            return original_column
    return None


def parse_money_value(value: object) -> float:
    text = clean_text(value)
    if not text:
        return 0.0
    text = text.replace(",", "").replace("￥", "").replace("¥", "").replace("元", "")
    try:
        return float(text)
    except ValueError:
        return 0.0


def parse_money(series: pd.Series) -> pd.Series:
    cleaned = (
        series.astype(str)
        .str.replace(",", "", regex=False)
        .str.replace("￥", "", regex=False)
        .str.replace("¥", "", regex=False)
        .str.replace("元", "", regex=False)
        .str.strip()
    )
    return pd.to_numeric(cleaned, errors="coerce").fillna(0)


def parse_collected(value: object) -> bool:
    text = clean_text(value).lower()
    return text in {"true", "1", "yes", "y", "是", "已收", "已收款", "收了", "收齐", "已付", "已付款", "paid"}


def extract_group_name(raw: pd.DataFrame, sheet_name: str, fallback: str) -> str:
    first_cell = clean_text(raw.iat[0, 0]) if raw.shape[0] and raw.shape[1] else ""
    if "【" in first_cell and "】" in first_cell:
        return first_cell.split("【", 1)[1].split("】", 1)[0].strip() or fallback
    if "汇总" in first_cell:
        return first_cell.split("汇总", 1)[0].strip(" -_，,") or fallback
    return fallback or sheet_name


def make_record_ids(frame: pd.DataFrame) -> pd.Series:
    parts = (
        frame["source_file"].astype(str)
        + "|"
        + frame["source_sheet"].astype(str)
        + "|"
        + frame["batch"].astype(str)
        + "|"
        + frame["cn"].astype(str)
        + "|"
        + frame["item_name"].astype(str)
        + "|"
        + frame["role"].astype(str)
    )
    counts = parts.groupby(parts).cumcount().astype(str)
    return (parts + "|" + counts).map(lambda value: hashlib.md5(value.encode("utf-8")).hexdigest())


def standardize_detail_frame(frame: pd.DataFrame, sheet_name: str, source_file: str) -> Tuple[pd.DataFrame, Dict[str, str]]:
    frame = frame.dropna(how="all").copy()
    mapping: Dict[str, str] = {}
    output = pd.DataFrame()

    for target, aliases in COLUMN_ALIASES.items():
        source = find_column(frame.columns, aliases)
        if source is not None:
            mapping[target] = str(source)
            output[target] = frame[source]
        else:
            output[target] = "" if target not in {"quantity", "unit_price", "amount"} else 0

    output["cn"] = output["cn"].map(clean_text)
    output["item_name"] = output["item_name"].map(clean_text)
    output["role"] = output["role"].map(clean_text)
    output["batch"] = output["batch"].map(clean_text)
    output["quantity"] = parse_money(output["quantity"]).replace(0, 1)
    output["unit_price"] = parse_money(output["unit_price"])
    output["amount"] = parse_money(output["amount"])

    missing_amount = output["amount"] <= 0
    output.loc[missing_amount, "amount"] = output.loc[missing_amount, "quantity"] * output.loc[missing_amount, "unit_price"]
    output["collected"] = output["collected"].apply(parse_collected)
    output["source_sheet"] = sheet_name
    output["source_file"] = source_file

    useful_row = output[["cn", "item_name", "role", "batch"]].replace({"": pd.NA}).notna().any(axis=1)
    output = output.loc[useful_row].reset_index(drop=True)
    if output.empty:
        return empty_records(), mapping

    output["record_id"] = make_record_ids(output)
    return output[REQUIRED_COLUMNS], mapping


def locate_header_row(raw: pd.DataFrame) -> Optional[int]:
    max_rows = min(len(raw), 30)
    alias_words = [normalize_name(alias) for aliases in COLUMN_ALIASES.values() for alias in aliases]
    for row_index in range(max_rows):
        values = [normalize_name(value) for value in raw.iloc[row_index].tolist()]
        score = sum(1 for value in values if value and any(alias == value or alias in value for alias in alias_words))
        if score >= 2:
            return row_index
    return None


def parse_detail_sheet(raw: pd.DataFrame, sheet_name: str, source_file: str) -> Tuple[pd.DataFrame, Dict[str, str]]:
    header_row = locate_header_row(raw)
    if header_row is None:
        return empty_records(), {}

    frame = raw.iloc[header_row + 1 :].copy()
    frame.columns = [clean_text(value) or "未命名列" for value in raw.iloc[header_row].tolist()]
    return standardize_detail_frame(frame, sheet_name, source_file)


def locate_matrix_rows(raw: pd.DataFrame) -> Optional[Tuple[int, int, int, int]]:
    kind_pos: Optional[Tuple[int, int]] = None
    price_pos: Optional[Tuple[int, int]] = None
    total_pos: Optional[Tuple[int, int]] = None

    for row_index in range(min(len(raw), 30)):
        for col_index in range(raw.shape[1]):
            text = normalize_name(raw.iat[row_index, col_index])
            if text == "种类":
                kind_pos = (row_index, col_index)
            elif text == "单价":
                price_pos = (row_index, col_index)
            elif text in {"昵称总数", "昵称/总数", "昵称"}:
                total_pos = (row_index, col_index)

    if kind_pos and price_pos:
        start_row = (total_pos[0] + 1) if total_pos else max(kind_pos[0], price_pos[0]) + 2
        return kind_pos[0], price_pos[0], kind_pos[1], start_row
    return None


def parse_matrix_sheet(raw: pd.DataFrame, sheet_name: str, source_file: str) -> Tuple[pd.DataFrame, Dict[str, str]]:
    located = locate_matrix_rows(raw)
    if not located:
        return empty_records(), {}

    kind_row, price_row, label_col, start_row = located
    group_name = extract_group_name(raw, sheet_name, Path(source_file).stem)
    cn_col = label_col
    total_col = max(0, label_col - 1)
    item_columns: List[Tuple[int, str, float]] = []

    for col_index in range(label_col + 1, raw.shape[1]):
        role = clean_text(raw.iat[kind_row, col_index])
        unit_price = parse_money_value(raw.iat[price_row, col_index])
        if not role and item_columns:
            break
        if role and unit_price > 0:
            item_columns.append((col_index, role, unit_price))

    rows = []
    for row_index in range(start_row, len(raw)):
        cn = clean_text(raw.iat[row_index, cn_col])
        if not cn:
            continue
        row_total = parse_money_value(raw.iat[row_index, total_col])
        if cn in {"昵称/总数", "昵称", "总数"}:
            continue

        for col_index, role, unit_price in item_columns:
            quantity = parse_money_value(raw.iat[row_index, col_index])
            if quantity <= 0:
                continue
            rows.append(
                {
                    "cn": cn,
                    "item_name": group_name,
                    "role": role,
                    "batch": group_name,
                    "quantity": quantity,
                    "unit_price": unit_price,
                    "amount": quantity * unit_price,
                    "collected": False,
                    "source_sheet": sheet_name,
                    "source_file": source_file,
                    "row_total": row_total,
                }
            )

    if not rows:
        return empty_records(), {}

    output = pd.DataFrame(rows)
    output["record_id"] = make_record_ids(output)
    return output[REQUIRED_COLUMNS], {"格式": "矩阵汇总表", "谷子名称": group_name, "角色列数量": str(len(item_columns))}


def empty_records() -> pd.DataFrame:
    return pd.DataFrame(columns=REQUIRED_COLUMNS)


@st.cache_data(show_spinner=False)
def read_excel(file_bytes: bytes, file_name: str) -> Tuple[pd.DataFrame, Dict[str, Dict[str, str]]]:
    workbook = pd.read_excel(BytesIO(file_bytes), sheet_name=None, header=None)
    frames = []
    mappings: Dict[str, Dict[str, str]] = {}

    for sheet_name, raw in workbook.items():
        matrix_records, matrix_mapping = parse_matrix_sheet(raw, sheet_name, file_name)
        if not matrix_records.empty:
            frames.append(matrix_records)
            mappings[sheet_name] = matrix_mapping
            continue

        detail_records, detail_mapping = parse_detail_sheet(raw, sheet_name, file_name)
        if not detail_records.empty:
            frames.append(detail_records)
            mappings[sheet_name] = detail_mapping

    if not frames:
        return empty_records(), mappings
    return pd.concat(frames, ignore_index=True), mappings


def config_value(*names: str) -> str:
    for name in names:
        try:
            if name in st.secrets:
                return str(st.secrets[name]).strip()
        except Exception:
            pass
        value = os.environ.get(name)
        if value:
            return value.strip()
    return ""


@st.cache_resource(show_spinner=False)
def get_supabase_client():
    if create_client is None:
        return None
    url = config_value("SUPABASE_URL")
    key = config_value("SUPABASE_SERVICE_ROLE_KEY", "SUPABASE_KEY", "SUPABASE_ANON_KEY")
    if not url or not key:
        return None
    return create_client(url, key)


def supabase_bucket_name() -> str:
    return config_value("PJSK_SUPABASE_BUCKET", "SUPABASE_BUCKET") or SUPABASE_BUCKET


def supabase_table_name(default: str, env_name: str) -> str:
    return config_value(env_name) or default


def supabase_enabled() -> bool:
    return get_supabase_client() is not None


def clean_supabase_value(value):
    if pd.isna(value):
        return None
    if hasattr(value, "item"):
        return value.item()
    return value


def frame_to_rows(frame: pd.DataFrame) -> List[Dict[str, object]]:
    rows = []
    for row in frame.to_dict("records"):
        rows.append({key: clean_supabase_value(value) for key, value in row.items()})
    return rows


def select_supabase_rows(table_name: str) -> List[Dict[str, object]]:
    client = get_supabase_client()
    if client is None:
        return []
    rows: List[Dict[str, object]] = []
    start = 0
    chunk_size = 1000
    while True:
        response = client.table(table_name).select("*").range(start, start + chunk_size - 1).execute()
        batch = response.data or []
        if not batch:
            break
        rows.extend(batch)
        if len(batch) < chunk_size:
            break
        start += chunk_size
    return rows


def replace_supabase_table(table_name: str, id_column: str, frame: pd.DataFrame) -> None:
    client = get_supabase_client()
    if client is None:
        return
    client.table(table_name).delete().neq(id_column, "__pjsk_never__").execute()
    rows = frame_to_rows(frame)
    for start in range(0, len(rows), 500):
        client.table(table_name).insert(rows[start : start + 500]).execute()


def normalize_records_frame(frame: pd.DataFrame) -> pd.DataFrame:
    for column in REQUIRED_COLUMNS:
        if column not in frame.columns:
            frame[column] = False if column == "collected" else ""
    frame["quantity"] = pd.to_numeric(frame["quantity"], errors="coerce").fillna(0)
    frame["unit_price"] = pd.to_numeric(frame["unit_price"], errors="coerce").fillna(0)
    frame["amount"] = pd.to_numeric(frame["amount"], errors="coerce").fillna(0)
    frame["collected"] = frame["collected"].astype(str).str.lower().isin(["true", "1", "yes", "是", "已收"])
    return frame[REQUIRED_COLUMNS]


def normalize_payment_frame(frame: pd.DataFrame) -> pd.DataFrame:
    for column in PAYMENT_COLUMNS:
        if column not in frame.columns:
            frame[column] = ""
    frame["amount"] = pd.to_numeric(frame["amount"], errors="coerce").fillna(0)
    frame["approved"] = frame["approved"].astype(str).str.lower().isin(["true", "1", "yes", "是", "已通过"])
    return frame[PAYMENT_COLUMNS]


def make_storage_ref(path: str, bucket: Optional[str] = None) -> str:
    return f"{SUPABASE_PREFIX}{bucket or supabase_bucket_name()}/{path}"


def split_storage_ref(ref: str) -> Tuple[str, str]:
    raw = str(ref).replace(SUPABASE_PREFIX, "", 1)
    if "/" not in raw:
        return supabase_bucket_name(), raw
    bucket, path = raw.split("/", 1)
    return bucket, path


def content_type_for_name(file_name: str) -> str:
    suffix = image_suffix(file_name)
    if suffix in {".jpg", ".jpeg"}:
        return "image/jpeg"
    if suffix == ".webp":
        return "image/webp"
    return "image/png"


def upload_storage_file(path: str, file_bytes: bytes, content_type: str, bucket: Optional[str] = None) -> str:
    client = get_supabase_client()
    if client is None:
        raise RuntimeError("Supabase is not configured")
    bucket_name = bucket or supabase_bucket_name()
    storage = client.storage.from_(bucket_name)
    options = {"content-type": content_type, "upsert": "true"}
    try:
        storage.upload(path, file_bytes, file_options=options)
    except Exception:
        storage.update(path, file_bytes, file_options=options)
    return make_storage_ref(path, bucket_name)


def save_uploaded_file_to_ref(uploaded_file, ref: str) -> str:
    file_bytes = uploaded_file.getvalue()
    content_type = getattr(uploaded_file, "type", None) or content_type_for_name(uploaded_file.name)
    if str(ref).startswith(SUPABASE_PREFIX):
        bucket, path = split_storage_ref(str(ref))
        return upload_storage_file(path, file_bytes, content_type, bucket)
    target = Path(str(ref))
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(file_bytes)
    return str(target)


def load_image_bytes(ref: object) -> Optional[bytes]:
    if not ref:
        return None
    ref_text = str(ref)
    if ref_text.startswith(SUPABASE_PREFIX):
        client = get_supabase_client()
        if client is None:
            return None
        bucket, path = split_storage_ref(ref_text)
        try:
            return client.storage.from_(bucket).download(path)
        except Exception:
            return None
    path = Path(ref_text)
    if not path.exists():
        return None
    return path.read_bytes()


def load_records() -> pd.DataFrame:
    if supabase_enabled():
        table_name = supabase_table_name(SUPABASE_RECORDS_TABLE, "PJSK_RECORDS_TABLE")
        try:
            return normalize_records_frame(pd.DataFrame(select_supabase_rows(table_name)))
        except Exception as exc:
            st.error(f"Supabase 读取明细失败：{exc}")
            return empty_records()
    if not DATA_PATH.exists():
        return empty_records()
    frame = pd.read_csv(DATA_PATH, dtype={"record_id": str})
    return normalize_records_frame(frame)


def save_records(frame: pd.DataFrame) -> None:
    frame = normalize_records_frame(frame.copy())
    if supabase_enabled():
        table_name = supabase_table_name(SUPABASE_RECORDS_TABLE, "PJSK_RECORDS_TABLE")
        try:
            replace_supabase_table(table_name, "record_id", frame)
            return
        except Exception as exc:
            st.error(f"Supabase 保存明细失败：{exc}")
            raise exc
    try:
        DATA_DIR.mkdir(parents=True, exist_ok=True)
        frame.to_csv(DATA_PATH, index=False, encoding="utf-8-sig")
    except PermissionError as exc:
        st.error(f"保存失败：当前电脑不允许写入数据文件夹。请检查这个路径的权限：{DATA_DIR}")
        raise exc


def append_records(new_records: pd.DataFrame) -> None:
    current = load_records()
    combined = pd.concat([current, new_records], ignore_index=True)
    combined = combined.drop_duplicates(subset=["record_id"], keep="last")
    save_records(combined)


def split_new_and_duplicate_records(records: pd.DataFrame) -> Tuple[pd.DataFrame, pd.DataFrame]:
    current = load_records()
    if current.empty:
        return records.copy(), empty_records()
    existing_ids = set(current["record_id"].astype(str))
    record_ids = records["record_id"].astype(str)
    new_records = records[~record_ids.isin(existing_ids)].copy()
    duplicate_records = records[record_ids.isin(existing_ids)].copy()
    return new_records, duplicate_records


def empty_payment_records() -> pd.DataFrame:
    return pd.DataFrame(columns=PAYMENT_COLUMNS)


def load_payment_records() -> pd.DataFrame:
    if supabase_enabled():
        table_name = supabase_table_name(SUPABASE_PAYMENTS_TABLE, "PJSK_PAYMENTS_TABLE")
        try:
            return normalize_payment_frame(pd.DataFrame(select_supabase_rows(table_name)))
        except Exception as exc:
            st.error(f"Supabase 读取交肾记录失败：{exc}")
            return empty_payment_records()
    if not PAYMENT_PATH.exists():
        return empty_payment_records()
    frame = pd.read_csv(PAYMENT_PATH, dtype={"payment_id": str})
    return normalize_payment_frame(frame)


def save_payment_records(frame: pd.DataFrame) -> None:
    frame = normalize_payment_frame(frame.copy())
    if supabase_enabled():
        table_name = supabase_table_name(SUPABASE_PAYMENTS_TABLE, "PJSK_PAYMENTS_TABLE")
        try:
            replace_supabase_table(table_name, "payment_id", frame)
            return
        except Exception as exc:
            st.error(f"Supabase 保存交肾记录失败：{exc}")
            raise exc
    DATA_DIR.mkdir(parents=True, exist_ok=True)
    frame.to_csv(PAYMENT_PATH, index=False, encoding="utf-8-sig")


def now_text() -> str:
    return datetime.now().strftime("%Y-%m-%d %H:%M:%S")


def make_payment_id(cn: str, amount: float, created_at: str) -> str:
    raw = f"{cn}|{amount}|{created_at}|{len(load_payment_records())}"
    return hashlib.md5(raw.encode("utf-8")).hexdigest()


def image_suffix(file_name: str) -> str:
    suffix = Path(file_name).suffix.lower()
    return suffix if suffix in {".png", ".jpg", ".jpeg", ".webp"} else ".png"


def save_uploaded_image(uploaded_file, folder: Path, stem: str) -> str:
    target_name = f"{stem}{image_suffix(uploaded_file.name)}"
    if supabase_enabled():
        storage_path = f"{folder.name}/{target_name}"
        content_type = getattr(uploaded_file, "type", None) or content_type_for_name(uploaded_file.name)
        return upload_storage_file(storage_path, uploaded_file.getvalue(), content_type)
    folder.mkdir(parents=True, exist_ok=True)
    target = folder / target_name
    target.write_bytes(uploaded_file.getvalue())
    return str(target)


def qr_paths() -> Dict[str, str]:
    if supabase_enabled():
        return {"支付宝": make_storage_ref("qr_codes/alipay.png"), "微信": make_storage_ref("qr_codes/wechat.png")}
    return {"支付宝": str(ALIPAY_QR_PATH), "微信": str(WECHAT_QR_PATH)}


def money(value: float) -> str:
    return f"¥{value:,.2f}"


def wechat_pay_amount(value: float) -> float:
    amount = Decimal(str(value or 0))
    return float((amount * Decimal("1.001")).quantize(Decimal("0.01"), rounding=ROUND_CEILING))


def add_wechat_amount_column(frame: pd.DataFrame) -> pd.DataFrame:
    output = display_frame(frame)
    output["微信交肾金额"] = frame["amount"].apply(wechat_pay_amount)
    return output


def unique_options(frame: pd.DataFrame, column: str) -> List[str]:
    if frame.empty:
        return []
    return sorted(value for value in frame[column].dropna().astype(str).unique() if value and value.lower() != "nan")


def display_frame(frame: pd.DataFrame) -> pd.DataFrame:
    visible_columns = [column for column in REQUIRED_COLUMNS if column != "record_id"]
    return frame[visible_columns].rename(columns=DISPLAY_NAMES)


def filter_records(frame: pd.DataFrame, prefix: str = "") -> pd.DataFrame:
    top_left, top_right = st.columns(2)
    with top_left:
        cn_keyword = st.text_input("搜索 CN", key=f"{prefix}cn")
        item_keyword = st.text_input("搜索谷子名称", key=f"{prefix}item")
    with top_right:
        selected_status = st.radio("收款状态", ["全部", "已收", "未收"], horizontal=True, key=f"{prefix}status")
        amount_scope = st.radio("金额统计范围", ["当前查询结果", "全部数据"], horizontal=True, key=f"{prefix}scope")

    filter_left, filter_right = st.columns(2)
    with filter_left:
        selected_roles = st.multiselect("角色", unique_options(frame, "role"), key=f"{prefix}role")
    with filter_right:
        selected_batches = st.multiselect("团购批次", unique_options(frame, "batch"), key=f"{prefix}batch")

    result = frame.copy()
    if cn_keyword:
        result = result[result["cn"].astype(str).str.contains(cn_keyword, case=False, na=False)]
    if item_keyword:
        result = result[result["item_name"].astype(str).str.contains(item_keyword, case=False, na=False)]
    if selected_roles:
        result = result[result["role"].astype(str).isin(selected_roles)]
    if selected_batches:
        result = result[result["batch"].astype(str).isin(selected_batches)]
    if selected_status == "已收":
        result = result[result["collected"]]
    if selected_status == "未收":
        result = result[~result["collected"]]

    amount_frame = result if amount_scope == "当前查询结果" else frame
    stat1, stat2, stat3, stat4 = st.columns(4)
    stat1.metric("查询结果", f"{len(result)} 条")
    stat2.metric("金额合计", money(float(amount_frame["amount"].sum())))
    stat3.metric("已收", money(float(amount_frame.loc[amount_frame["collected"], "amount"].sum())))
    stat4.metric("未收", money(float(amount_frame.loc[~amount_frame["collected"], "amount"].sum())))
    return result


def to_excel_download(frame: pd.DataFrame) -> bytes:
    output = BytesIO()
    with pd.ExcelWriter(output, engine="openpyxl") as writer:
        display_frame(frame).to_excel(writer, index=False, sheet_name="查询结果")
    return output.getvalue()


def dataframe_to_excel(frame: pd.DataFrame, sheet_name: str = "导出数据") -> bytes:
    output = BytesIO()
    with pd.ExcelWriter(output, engine="openpyxl") as writer:
        frame.to_excel(writer, index=False, sheet_name=sheet_name[:31])
    return output.getvalue()


def render_query_center(frame: pd.DataFrame, prefix: str = "query_", show_wechat_amount: bool = False) -> None:
    st.subheader("查询中心")
    st.caption("组合查询后，可以直接确认明细、按 CN 汇总、按谷子汇总和导出结果。")
    result = filter_records(frame, prefix)
    if show_wechat_amount:
        st.warning("微信交肾需另交手续费：微信金额 = 原金额 * 0.001 + 原金额。下方微信金额仅供交肾参考，不纳入原始金额总和。")
        st.metric("当前查询结果微信交肾金额", money(wechat_pay_amount(float(result["amount"].sum()))))

    detail_tab, cn_tab, item_tab = st.tabs(["明细表", "按 CN 汇总", "按谷子汇总"])
    with detail_tab:
        detail_output = add_wechat_amount_column(result) if show_wechat_amount else display_frame(result)
        st.dataframe(detail_output, use_container_width=True, hide_index=True)
        st.download_button(
            "导出当前查询结果",
            data=dataframe_to_excel(detail_output, "查询结果") if show_wechat_amount else to_excel_download(result),
            file_name="当前查询结果.xlsx",
            mime="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
            disabled=result.empty,
            key=f"{prefix}download",
        )
    with cn_tab:
        cn_summary = (
            result.groupby("cn", dropna=False)
            .agg(数量=("quantity", "sum"), 金额=("amount", "sum"), 未收金额=("amount", lambda v: v[~result.loc[v.index, "collected"]].sum()))
            .reset_index()
            .rename(columns={"cn": "CN"})
            .sort_values("金额", ascending=False)
        )
        if show_wechat_amount:
            cn_summary["微信交肾金额"] = cn_summary["金额"].apply(wechat_pay_amount)
        st.dataframe(cn_summary, use_container_width=True, hide_index=True)
        st.download_button(
            "导出按 CN 汇总",
            data=dataframe_to_excel(cn_summary, "按CN汇总"),
            file_name="按CN汇总.xlsx",
            mime="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
            disabled=cn_summary.empty,
            key=f"{prefix}cn_summary_download",
        )
    with item_tab:
        item_summary = (
            result.groupby(["item_name", "role", "batch"], dropna=False)
            .agg(数量=("quantity", "sum"), 金额=("amount", "sum"), 涉及CN=("cn", "nunique"))
            .reset_index()
            .rename(columns={"item_name": "谷子名称", "role": "角色", "batch": "团购批次"})
            .sort_values("金额", ascending=False)
        )
        if show_wechat_amount:
            item_summary["微信交肾金额"] = item_summary["金额"].apply(wechat_pay_amount)
        st.dataframe(item_summary, use_container_width=True, hide_index=True)


def render_import_center() -> None:
    st.subheader("导入中心")
    uploaded_file = st.file_uploader("导入 Excel", type=["xlsx"])
    if not uploaded_file:
        return

    records, mappings = read_excel(uploaded_file.getvalue(), uploaded_file.name)
    if records.empty:
        st.error("没有识别到可用数据。这个文件可能不是普通明细表或当前支持的矩阵汇总表。")
        return

    new_records, duplicate_records = split_new_and_duplicate_records(records)
    st.success(f"本次文件共识别到 {len(records)} 条明细，其中新增 {len(new_records)} 条，重复 {len(duplicate_records)} 条。")
    if duplicate_records.empty:
        st.info("没有发现重复记录。")
    else:
        st.warning("重复记录已自动跳过，不会出现在新增预览里，也不会被追加保存。")

    with st.expander("字段识别结果", expanded=True):
        st.json(mappings)
    with st.expander("新增记录预览", expanded=True):
        if new_records.empty:
            st.info("这次上传没有新增记录。比如 Sheet1 已经上传过，就会被自动跳过。")
        else:
            st.dataframe(display_frame(new_records), use_container_width=True, hide_index=True)
    if not duplicate_records.empty:
        with st.expander("已跳过的重复记录", expanded=False):
            st.dataframe(display_frame(duplicate_records), use_container_width=True, hide_index=True)

    col1, col2 = st.columns(2)
    with col1:
        if st.button("只追加保存新增记录", type="primary", use_container_width=True, disabled=new_records.empty):
            append_records(new_records)
            st.success("新增记录已保存。普通查询端刷新后也能看到这些数据。")
            st.rerun()
    with col2:
        if st.button("清空旧数据后保存", use_container_width=True):
            save_records(records)
            st.success("已用本次导入覆盖旧数据。")
            st.rerun()


def render_collection_admin(frame: pd.DataFrame) -> None:
    st.subheader("交肾状态管理")
    result = filter_records(frame, "collect_")
    if st.button("一键全选当前查询结果为已收", disabled=result.empty):
        updated = frame.copy()
        target_ids = set(result["record_id"].astype(str))
        updated["record_id"] = updated["record_id"].astype(str)
        updated.loc[updated["record_id"].isin(target_ids), "collected"] = True
        save_records(updated)
        st.success("当前查询结果已全部标记为已收。")
        st.rerun()

    editable = result[["record_id", "cn", "item_name", "role", "batch", "quantity", "amount", "collected"]].copy()
    editable = editable.rename(columns=DISPLAY_NAMES)
    edited = st.data_editor(
        editable,
        use_container_width=True,
        hide_index=True,
        disabled=["记录编号", "CN", "谷子名称", "角色", "团购批次", "数量", "金额"],
        column_config={"是否已收": st.column_config.CheckboxColumn("是否已收")},
    )

    if st.button("保存交肾状态", type="primary"):
        updated = frame.copy()
        status_map = dict(zip(edited["记录编号"].astype(str), edited["是否已收"].astype(bool)))
        updated["record_id"] = updated["record_id"].astype(str)
        updated.loc[updated["record_id"].isin(status_map.keys()), "collected"] = updated.loc[
            updated["record_id"].isin(status_map.keys()), "record_id"
        ].map(status_map)
        save_records(updated)
        st.success("交肾状态已保存。")
        st.rerun()


def render_reconciliation_center(frame: pd.DataFrame) -> None:
    st.subheader("对账中心")
    unpaid = frame[~frame["collected"]].copy()
    tab1, tab2, tab3 = st.tabs(["未收明细", "尾款人员", "催款清单"])

    with tab1:
        st.dataframe(display_frame(unpaid), use_container_width=True, hide_index=True)
    with tab2:
        arrears = (
            unpaid.groupby("cn", dropna=False)
            .agg(未收数量=("quantity", "sum"), 尾款金额=("amount", "sum"), 涉及谷子=("item_name", lambda v: "、".join(sorted(set(v.astype(str))))))
            .reset_index()
            .rename(columns={"cn": "CN"})
            .sort_values("尾款金额", ascending=False)
        )
        st.dataframe(arrears, use_container_width=True, hide_index=True)
    with tab3:
        st.download_button(
            "导出 Excel 催款清单",
            data=to_excel_download(unpaid),
            file_name="催款清单.xlsx",
            mime="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
            disabled=unpaid.empty,
        )


def render_series_summary_center(frame: pd.DataFrame) -> None:
    st.subheader("系列金额汇总")
    summary = (
        frame.groupby(["item_name", "batch"], dropna=False)
        .agg(
            数量=("quantity", "sum"),
            总金额=("amount", "sum"),
            已收金额=("amount", lambda values: values[frame.loc[values.index, "collected"]].sum()),
            未收金额=("amount", lambda values: values[~frame.loc[values.index, "collected"]].sum()),
            涉及CN=("cn", "nunique"),
            明细条数=("record_id", "count"),
        )
        .reset_index()
        .rename(columns={"item_name": "谷子系列名", "batch": "团购批次"})
        .sort_values("总金额", ascending=False)
    )
    st.dataframe(summary, use_container_width=True, hide_index=True)
    st.download_button(
        "导出系列金额汇总",
        data=dataframe_to_excel(summary, "系列金额汇总"),
        file_name="系列金额汇总.xlsx",
        mime="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        disabled=summary.empty,
    )


def render_qr_settings_admin() -> None:
    st.subheader("收款码设置")
    st.caption("这里上传的是收款二维码图片。普通端只展示图片，不会调用支付宝或微信接口。")

    col1, col2 = st.columns(2)
    uploads = [
        ("支付宝", qr_paths()["支付宝"], col1),
        ("微信", qr_paths()["微信"], col2),
    ]

    for label, ref, column in uploads:
        with column:
            st.markdown(f"#### {label}")
            image_bytes = load_image_bytes(ref)
            if image_bytes:
                st.image(image_bytes, use_container_width=True)
            uploaded = st.file_uploader(f"上传{label}收款码", type=["png", "jpg", "jpeg", "webp"], key=f"qr_{label}")
            if uploaded and st.button(f"保存{label}收款码", key=f"save_qr_{label}", use_container_width=True):
                save_uploaded_file_to_ref(uploaded, ref)
                st.success(f"{label}收款码已保存。")
                st.rerun()


def cn_amount_summary(frame: pd.DataFrame, cn: str) -> Tuple[pd.DataFrame, float, float]:
    current = frame[frame["cn"].astype(str) == cn].copy()
    total_amount = float(current["amount"].sum())
    unpaid_amount = float(current.loc[~current["collected"], "amount"].sum())
    return current, total_amount, unpaid_amount


def render_payment_user(frame: pd.DataFrame) -> None:
    st.subheader("交肾处")
    st.caption("确认金额后自行扫码付款，再上传付款截图。这里不接入支付宝或微信 API。")
    st.warning("微信交肾需另交手续费：微信金额 = 原金额 * 0.001 + 原金额。")

    cn_options = unique_options(frame, "cn")
    selected_cn = st.selectbox("选择你的 CN", [""] + cn_options)
    if not selected_cn:
        st.info("先选择 CN，就能看到对应金额、收款码和自己的截图记录。")
        return

    cn_rows, total_amount, unpaid_amount = cn_amount_summary(frame, selected_cn)
    wechat_unpaid_amount = wechat_pay_amount(unpaid_amount)
    col1, col2, col3, col4 = st.columns(4)
    col1.metric("总金额", money(total_amount))
    col2.metric("支付宝未交金额", money(unpaid_amount))
    col3.metric("微信未交金额", money(wechat_unpaid_amount))
    col4.metric("明细条数", len(cn_rows))

    with st.expander("查看我的明细", expanded=False):
        st.dataframe(add_wechat_amount_column(cn_rows), use_container_width=True, hide_index=True)

    st.markdown("### 收款码")
    qr_cols = st.columns(2)
    for index, (label, ref) in enumerate(qr_paths().items()):
        with qr_cols[index]:
            st.markdown(f"#### {label}")
            image_bytes = load_image_bytes(ref)
            if image_bytes:
                st.image(image_bytes, use_container_width=True)
            else:
                st.info(f"管理员还没有上传{label}收款码。")

    st.markdown("### 上传交肾截图")
    confirm_cn = st.text_input("CN（必填）", value=selected_cn)
    item_options = unique_options(cn_rows, "item_name")
    selected_items = st.multiselect(
        "谷子系列名（必填，可多选）",
        item_options,
        default=item_options[:1] if len(item_options) == 1 else [],
        help="可以同时勾选多个谷子系列。",
    )
    method = st.radio("付款方式", ["支付宝", "微信", "其他"], horizontal=True)
    recommended_amount = wechat_unpaid_amount if method == "微信" else max(unpaid_amount, 0.0)
    if method == "微信":
        st.error(f"微信交肾需交手续费。本次微信应交金额：{money(recommended_amount)}，计算方式：原金额 * 0.001 + 原金额。")
    amount = st.number_input(
        "本次截图对应金额",
        min_value=0.0,
        value=recommended_amount,
        step=0.01,
        format="%.2f",
        key=f"payment_amount_{selected_cn}_{method}",
    )
    note = st.text_input("备注", placeholder="可填转账时间、昵称或说明")
    screenshot = st.file_uploader("上传付款截图", type=["png", "jpg", "jpeg", "webp"], key=f"payment_upload_{selected_cn}")
    can_submit = bool(confirm_cn.strip()) and bool(selected_items) and screenshot is not None

    if st.button("提交交肾截图", type="primary", disabled=not can_submit):
        created_at = now_text()
        item_list = "、".join(selected_items)
        payment_id = make_payment_id(confirm_cn.strip(), amount, created_at)
        image_path = save_uploaded_image(screenshot, PAYMENT_IMAGE_DIR, payment_id)
        payments = load_payment_records()
        new_row = pd.DataFrame(
            [
                {
                    "payment_id": payment_id,
                    "cn": confirm_cn.strip(),
                    "item_list": item_list,
                    "amount": amount,
                    "method": method,
                    "note": note,
                    "image_path": image_path,
                    "approved": False,
                    "approved_at": "",
                    "created_at": created_at,
                    "updated_at": created_at,
                }
            ]
        )
        save_payment_records(pd.concat([payments, new_row], ignore_index=True))
        st.success("交肾截图已提交。")
        st.rerun()

    st.markdown("### 我的交肾截图记录")
    payments = load_payment_records()
    mine = payments[payments["cn"].astype(str) == selected_cn].copy()
    if mine.empty:
        st.info("当前 CN 还没有提交过截图。")
        return

    st.dataframe(mine.drop(columns=["image_path"]), use_container_width=True, hide_index=True)
    selected_payment_id = st.selectbox("选择要查看/修改的记录", mine["payment_id"].tolist())
    selected = mine[mine["payment_id"] == selected_payment_id].iloc[0]
    approved = bool(selected.get("approved", False))
    image_bytes = load_image_bytes(selected["image_path"])
    if image_bytes:
        st.image(image_bytes, caption="当前截图", use_container_width=True)

    if approved:
        st.success("这条交肾记录已由管理员通过，截图已锁定，不能再替换。需要补充图片的话，可以在上方重新提交一条新的交肾截图记录。")
        return

    replacement = st.file_uploader("替换这条记录的截图", type=["png", "jpg", "jpeg", "webp"], key=f"replace_{selected_payment_id}")
    if st.button("保存替换截图", disabled=replacement is None):
        new_path = save_uploaded_image(replacement, PAYMENT_IMAGE_DIR, selected_payment_id)
        updated = payments.copy()
        mask = updated["payment_id"].astype(str) == selected_payment_id
        updated.loc[mask, "image_path"] = new_path
        updated.loc[mask, "updated_at"] = now_text()
        save_payment_records(updated)
        st.success("截图已更新。")
        st.rerun()


def render_payment_records_admin() -> None:
    st.subheader("交肾记录")
    payments = load_payment_records()
    if payments.empty:
        st.info("还没有普通用户提交交肾截图。")
        return

    st.dataframe(payments.drop(columns=["image_path"]), use_container_width=True, hide_index=True)
    st.download_button(
        "导出交肾记录",
        data=dataframe_to_excel(payments.drop(columns=["image_path"]), "交肾记录"),
        file_name="交肾记录.xlsx",
        mime="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    )

    selected_payment_id = st.selectbox("查看截图", payments["payment_id"].tolist())
    selected = payments[payments["payment_id"] == selected_payment_id].iloc[0]
    approved = bool(selected.get("approved", False))
    status_text = "已通过" if approved else "待审核"
    st.write(f"CN：{selected['cn']}，金额：{money(float(selected['amount']))}，方式：{selected['method']}，状态：{status_text}")

    approve_col, cancel_col = st.columns(2)
    with approve_col:
        if st.button("标记为已通过", type="primary", disabled=approved, use_container_width=True):
            updated = payments.copy()
            mask = updated["payment_id"].astype(str) == selected_payment_id
            updated.loc[mask, "approved"] = True
            updated.loc[mask, "approved_at"] = now_text()
            updated.loc[mask, "updated_at"] = now_text()
            save_payment_records(updated)
            st.success("这条交肾记录已通过，普通用户将不能再替换这张截图。")
            st.rerun()
    with cancel_col:
        if st.button("取消通过", disabled=not approved, use_container_width=True):
            updated = payments.copy()
            mask = updated["payment_id"].astype(str) == selected_payment_id
            updated.loc[mask, "approved"] = False
            updated.loc[mask, "approved_at"] = ""
            updated.loc[mask, "updated_at"] = now_text()
            save_payment_records(updated)
            st.success("已取消通过，这条记录可以由普通用户重新替换截图。")
            st.rerun()

    image_bytes = load_image_bytes(selected["image_path"])
    if image_bytes:
        st.image(image_bytes, use_container_width=True)
    else:
        st.warning("这条记录的截图文件不存在。")


def run_app(role: str) -> None:
    is_admin = role == "admin"
    title = "谷子管理系统 - 管理员端" if is_admin else "谷子查询系统 - 普通端"
    st.set_page_config(page_title=title, page_icon="📦", layout="wide")
    st.title("📦 " + title)

    frame = load_records()
    if is_admin:
        pages = ["导入中心", "查询中心", "系列金额汇总", "收款码设置", "交肾记录", "交肾状态管理", "对账中心"]
    else:
        pages = ["查询中心", "交肾处", "对账中心"]
    page = st.sidebar.radio("功能中心", pages)

    if is_admin and page == "导入中心":
        render_import_center()
        if not frame.empty:
            st.markdown("### 当前数据中心")
            st.dataframe(display_frame(frame), use_container_width=True, hide_index=True)
        return

    if is_admin and page == "收款码设置":
        render_qr_settings_admin()
        return

    if is_admin and page == "交肾记录":
        render_payment_records_admin()
        return

    if frame.empty:
        st.info("当前还没有可查询的数据。请先让管理员端导入并保存 Excel。")
        return

    if page == "查询中心":
        render_query_center(frame, show_wechat_amount=not is_admin)
    elif page == "系列金额汇总" and is_admin:
        render_series_summary_center(frame)
    elif page == "收款码设置" and is_admin:
        render_qr_settings_admin()
    elif page == "交肾记录" and is_admin:
        render_payment_records_admin()
    elif page == "交肾处":
        render_payment_user(frame)
    elif page == "交肾状态管理" and is_admin:
        render_collection_admin(frame)
    elif page == "对账中心":
        render_reconciliation_center(frame)
