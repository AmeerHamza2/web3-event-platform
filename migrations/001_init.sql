-- Chain Monitor schema. The service applies this idempotently on startup; it is
-- also kept here for reference and for running migrations independently.

CREATE TABLE IF NOT EXISTS blocks (
    number      BIGINT PRIMARY KEY,
    hash        TEXT   NOT NULL,
    parent_hash TEXT   NOT NULL,
    block_time  BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS transfers (
    id           BIGSERIAL PRIMARY KEY,
    block_number BIGINT  NOT NULL REFERENCES blocks(number) ON DELETE CASCADE,
    token        TEXT    NOT NULL,
    from_addr    TEXT    NOT NULL,
    to_addr      TEXT    NOT NULL,
    value        NUMERIC NOT NULL,
    tx_hash      TEXT    NOT NULL,
    log_index    BIGINT  NOT NULL,
    confirmed    BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (tx_hash, log_index)
);

CREATE INDEX IF NOT EXISTS idx_transfers_block ON transfers(block_number);
CREATE INDEX IF NOT EXISTS idx_transfers_confirmed ON transfers(confirmed);
