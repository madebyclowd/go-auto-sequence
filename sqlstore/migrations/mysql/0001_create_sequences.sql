-- go-auto-sequence: counter table (MySQL 8+).
-- One row per (name, scope, period); counter is the raw count of numbers issued.
-- The key columns use a binary, NO PAD collation on purpose: with MySQL's default collation
-- "Invoice" and "invoice", or "a" and "a ", would silently share one counter.
CREATE TABLE IF NOT EXISTS sequences (
    name       varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
    scope      varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL DEFAULT '',
    period     varchar(32)  CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL DEFAULT '',
    counter    bigint       NOT NULL,
    updated_at timestamp    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (name, scope, period),
    CONSTRAINT sequences_counter_nonneg CHECK (counter >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
