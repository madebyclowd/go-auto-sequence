-- go-auto-sequence: counter table (PostgreSQL).
-- One row per (name, scope, period); counter is the raw count of numbers issued.
CREATE TABLE IF NOT EXISTS sequences (
    name       varchar(128) NOT NULL,
    scope      varchar(128) NOT NULL DEFAULT '',
    period     varchar(32)  NOT NULL DEFAULT '',
    counter    bigint       NOT NULL,
    updated_at timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (name, scope, period)
);
