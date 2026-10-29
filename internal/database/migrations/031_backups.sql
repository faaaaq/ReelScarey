CREATE TABLE backup_schedule (
    id INTEGER PRIMARY KEY CHECK(id=1),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN(0,1)),
    interval_days INTEGER NOT NULL DEFAULT 7 CHECK(interval_days BETWEEN 1 AND 30),
    keep_count INTEGER NOT NULL DEFAULT 10 CHECK(keep_count BETWEEN 1 AND 100),
    keep_days INTEGER NOT NULL DEFAULT 30 CHECK(keep_days BETWEEN 1 AND 365),
    next_run INTEGER NOT NULL DEFAULT 0,
    last_attempt INTEGER NOT NULL DEFAULT 0,
    last_success INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    running INTEGER NOT NULL DEFAULT 0 CHECK(running IN(0,1)),
    revision INTEGER NOT NULL DEFAULT 1
);
INSERT INTO backup_schedule(id) VALUES(1);
