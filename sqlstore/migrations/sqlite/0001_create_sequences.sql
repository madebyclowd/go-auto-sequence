-- go-auto-sequence: counter table (SQLite 3.35+).
-- One row per (name, scope, period); counter is the raw count of numbers issued. The CHECK also
-- catches integer overflow: SQLite silently turns an overflowing integer addition into a REAL.
CREATE TABLE IF NOT EXISTS sequences (
    name       TEXT    NOT NULL,
    scope      TEXT    NOT NULL DEFAULT '',
    period     TEXT    NOT NULL DEFAULT '',
    counter    INTEGER NOT NULL CHECK (typeof(counter) = 'integer' AND counter >= 0),
    updated_at TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (name, scope, period)
) WITHOUT ROWID;
