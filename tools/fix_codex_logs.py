import os
import sqlite3
import time
from datetime import datetime
from pathlib import Path


def wal_size(db_path: Path) -> int:
    wal = Path(str(db_path) + "-wal")
    return wal.stat().st_size if wal.exists() else 0


def get_stats(cur: sqlite3.Cursor) -> tuple[int, int]:
    max_id, count = cur.execute("select max(id), count(*) from logs").fetchone()
    return int(max_id or 0), int(count or 0)


def main() -> None:
    db_path = Path(os.path.expanduser("~/.codex/logs_2.sqlite"))
    if not db_path.exists():
        raise SystemExit(f"missing database: {db_path}")

    timestamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    backup_path = db_path.with_name(f"{db_path.name}.backup-{timestamp}")

    source = sqlite3.connect(str(db_path))
    backup = sqlite3.connect(str(backup_path))
    try:
        source.backup(backup)
    finally:
        backup.close()

    cur = source.cursor()
    before = get_stats(cur)
    before_wal = wal_size(db_path)

    cur.execute("drop trigger if exists codex_block_logs_insert")
    cur.execute(
        """
        create trigger codex_block_logs_insert
        before insert on logs
        begin
            select raise(ignore);
        end
        """
    )
    source.commit()
    checkpoint = cur.execute("pragma wal_checkpoint(truncate)").fetchall()

    after_trigger = get_stats(cur)
    after_trigger_wal = wal_size(db_path)
    time.sleep(5)
    after_sample = get_stats(cur)
    after_sample_wal = wal_size(db_path)

    print(f"database={db_path}")
    print(f"backup={backup_path}")
    print(f"before max_id/count/wal={before[0]}/{before[1]}/{before_wal}")
    print(f"checkpoint={checkpoint}")
    print(f"after trigger max_id/count/wal={after_trigger[0]}/{after_trigger[1]}/{after_trigger_wal}")
    print(f"after 5s max_id/count/wal={after_sample[0]}/{after_sample[1]}/{after_sample_wal}")
    print(
        "growth "
        f"max_id={after_sample[0] - after_trigger[0]} "
        f"count={after_sample[1] - after_trigger[1]} "
        f"wal={after_sample_wal - after_trigger_wal}"
    )
    source.close()


if __name__ == "__main__":
    main()
