from __future__ import annotations

import csv
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FIXTURE_DIR = ROOT / "testdata" / "excel"
MANIFEST_PATH = FIXTURE_DIR / "manifest.csv"
sys.path.insert(0, str(ROOT / "legacy-streamlit"))

from app_core import read_excel


def optional_int(row: dict[str, str], key: str) -> int | None:
    value = row.get(key, "").strip()
    return int(value) if value else None


def optional_float(row: dict[str, str], key: str) -> float | None:
    value = row.get(key, "").strip()
    return float(value) if value else None


def optional_list(row: dict[str, str], key: str) -> list[str] | None:
    value = row.get(key, "").strip()
    return value.split("|") if value else None


def main() -> int:
    failures: list[str] = []

    with MANIFEST_PATH.open("r", encoding="utf-8-sig", newline="") as handle:
        rows = list(csv.DictReader(handle))

    for row in rows:
        file_name = row["file_name"]
        expected_rows = int(row["expected_rows"])
        expected_sheet_count = optional_int(row, "expected_sheet_count")
        expected_cn_count = optional_int(row, "expected_cn_count")
        expected_amount_sum = optional_float(row, "expected_amount_sum")
        expected_source_sheets = optional_list(row, "expected_source_sheets")
        fixture_path = FIXTURE_DIR / file_name

        if not fixture_path.exists():
            failures.append(f"missing fixture: {fixture_path}")
            continue

        parsed, mappings = read_excel(fixture_path.read_bytes(), fixture_path.name)
        actual_rows = len(parsed)
        actual_sheet_count = len(mappings)
        actual_cn_count = int(parsed["cn"].nunique()) if not parsed.empty else 0
        actual_amount_sum = round(float(parsed["amount"].sum()), 2) if not parsed.empty else 0.0
        actual_source_sheets = sorted(parsed["source_sheet"].astype(str).unique().tolist()) if not parsed.empty else []

        print(
            f"{file_name}: rows={actual_rows}, sheets={actual_sheet_count}, "
            f"cn={actual_cn_count}, amount_sum={actual_amount_sum}"
        )

        if actual_rows != expected_rows:
            failures.append(f"{file_name}: expected rows {expected_rows}, got {actual_rows}")
        if expected_sheet_count is not None and actual_sheet_count != expected_sheet_count:
            failures.append(f"{file_name}: expected sheets {expected_sheet_count}, got {actual_sheet_count}")
        if expected_cn_count is not None and actual_cn_count != expected_cn_count:
            failures.append(f"{file_name}: expected cn_count {expected_cn_count}, got {actual_cn_count}")
        if expected_amount_sum is not None and actual_amount_sum != round(expected_amount_sum, 2):
            failures.append(f"{file_name}: expected amount_sum {expected_amount_sum}, got {actual_amount_sum}")
        if expected_source_sheets is not None and actual_source_sheets != sorted(expected_source_sheets):
            failures.append(
                f"{file_name}: expected source_sheets {sorted(expected_source_sheets)}, got {actual_source_sheets}"
            )

    if failures:
        print("")
        print("fixture verification failed:")
        for failure in failures:
            print(f"- {failure}")
        return 1

    print("")
    print("fixture verification passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
