package chainmonitor

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schema is idempotent so the service can run it on startup. ON DELETE CASCADE
// lets a reorg rollback delete blocks and have their transfers removed with them.
const schema = `
CREATE TABLE IF NOT EXISTS blocks (
    number      BIGINT PRIMARY KEY,
    hash        TEXT   NOT NULL,
    parent_hash TEXT   NOT NULL,
    block_time  BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS transfers (
    id           BIGSERIAL PRIMARY KEY,
    block_number BIGINT NOT NULL REFERENCES blocks(number) ON DELETE CASCADE,
    token        TEXT   NOT NULL,
    from_addr    TEXT   NOT NULL,
    to_addr      TEXT   NOT NULL,
    value        NUMERIC NOT NULL,
    tx_hash      TEXT   NOT NULL,
    log_index    BIGINT NOT NULL,
    confirmed    BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (tx_hash, log_index)
);
CREATE INDEX IF NOT EXISTS idx_transfers_block ON transfers(block_number);
CREATE INDEX IF NOT EXISTS idx_transfers_confirmed ON transfers(confirmed);
`

// PostgresStore is a Postgres-backed Store.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects, applies the schema, and returns the store.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

// SaveBlock upserts the block and inserts its transfers in one transaction.
func (s *PostgresStore) SaveBlock(ctx context.Context, b Block, transfers []Transfer, confirmed bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit

	if _, err := tx.Exec(ctx,
		`INSERT INTO blocks (number, hash, parent_hash, block_time)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (number) DO UPDATE
		   SET hash = EXCLUDED.hash, parent_hash = EXCLUDED.parent_hash, block_time = EXCLUDED.block_time`,
		b.Number, b.Hash.Hex(), b.ParentHash.Hex(), b.Time,
	); err != nil {
		return fmt.Errorf("upsert block: %w", err)
	}

	for _, t := range transfers {
		if _, err := tx.Exec(ctx,
			`INSERT INTO transfers (block_number, token, from_addr, to_addr, value, tx_hash, log_index, confirmed)
			 VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8)
			 ON CONFLICT (tx_hash, log_index) DO NOTHING`,
			b.Number, t.Token.Hex(), t.From.Hex(), t.To.Hex(), t.Value.String(), t.TxHash.Hex(), t.LogIndex, confirmed,
		); err != nil {
			return fmt.Errorf("insert transfer: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) LastBlock(ctx context.Context) (Block, bool, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT number, hash, parent_hash, block_time FROM blocks ORDER BY number DESC LIMIT 1`)
	b, err := scanBlock(row)
	if err == pgx.ErrNoRows {
		return Block{}, false, nil
	}
	if err != nil {
		return Block{}, false, err
	}
	return b, true, nil
}

func (s *PostgresStore) BlockByNumber(ctx context.Context, number uint64) (Block, bool, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT number, hash, parent_hash, block_time FROM blocks WHERE number = $1`, number)
	b, err := scanBlock(row)
	if err == pgx.ErrNoRows {
		return Block{}, false, nil
	}
	if err != nil {
		return Block{}, false, err
	}
	return b, true, nil
}

func (s *PostgresStore) DeleteFrom(ctx context.Context, number uint64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM blocks WHERE number >= $1`, number)
	return err
}

func (s *PostgresStore) ConfirmThrough(ctx context.Context, number uint64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE transfers SET confirmed = TRUE WHERE block_number <= $1 AND confirmed = FALSE`, number)
	return err
}

func scanBlock(row pgx.Row) (Block, error) {
	var (
		b           Block
		hash, phash string
	)
	if err := row.Scan(&b.Number, &hash, &phash, &b.Time); err != nil {
		return Block{}, err
	}
	b.Hash = common.HexToHash(hash)
	b.ParentHash = common.HexToHash(phash)
	return b, nil
}
