package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

// DBTX is the subset of database/sql the store needs. It is satisfied by *sql.DB, *sql.Tx and
// *sql.Conn, and is a strict subset of sqlc's DBTX and of GORM's ConnPool.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Option configures New.
type Option func(*config)

type config struct {
	table     string
	requireTx bool
}

// WithTables sets the counter table name, bare or schema-qualified. Default: "sequences".
func WithTables(sequences string) Option { return func(c *config) { c.table = sequences } }

// RequireTx makes Incr and Set return sequence.ErrNoTransaction unless the store is bound to a
// transaction (*sql.Tx), which guarantees gapless numbers: a rollback gives the number back.
// A *sql.DB or *sql.Conn counts as not transactional. Detection is by capability: a DBTX that
// cannot begin a transaction (no BeginTx method) is assumed to be one, so a custom wrapper
// around a pool must expose BeginTx, or RequireTx cannot protect you.
func RequireTx() Option { return func(c *config) { c.requireTx = true } }

// Store implements sequence.Store on database/sql. It is immutable and safe for concurrent
// use whenever its DBTX is.
type Store struct {
	db        DBTX
	dialect   Dialect
	query     string
	current   string
	set       string
	requireTx bool
	inTx      bool
}

var (
	_ sequence.Store    = (*Store)(nil)
	_ sequence.Resetter = (*Store)(nil)
)

// New returns a Store on db using dialect d. Configuration errors wrap
// sequence.ErrInvalidConfig.
func New(db DBTX, d Dialect, opts ...Option) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: db must not be nil", sequence.ErrInvalidConfig)
	}
	if !d.valid() {
		return nil, fmt.Errorf("%w: unknown dialect %v", sequence.ErrInvalidConfig, d)
	}
	cfg := config{table: defaultTable}
	for _, o := range opts {
		o(&cfg)
	}
	if err := validateTable(cfg.table); err != nil {
		return nil, err
	}
	return &Store{db: db, dialect: d, query: d.incrSQL(cfg.table), current: d.currentSQL(cfg.table), set: d.setSQL(cfg.table), requireTx: cfg.requireTx, inTx: isTx(db)}, nil
}

// WithTx returns a copy bound to tx, typically a *sql.Tx you manage. The receiver is
// unchanged, and the store never begins, commits or rolls back anything.
func (s *Store) WithTx(tx DBTX) *Store {
	c := *s
	c.db, c.inTx = tx, isTx(tx)
	return &c
}

// InTx reports whether the store is bound to a transaction (see WithTx). sequence.Prefetch uses
// it to refuse a transaction-bound store, which cannot be gapless once numbers are cached.
func (s *Store) InTx() bool { return s.inTx }

// isTx reports whether db is a transaction. Pools (*sql.DB) and connections (*sql.Conn) can
// begin a transaction; a *sql.Tx cannot.
func isTx(db DBTX) bool {
	_, pool := db.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	return !pool
}

// Incr implements sequence.Store with one atomic upsert.
func (s *Store) Incr(ctx context.Context, k sequence.Key, period string, by int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if by <= 0 {
		return 0, fmt.Errorf("%w: increment must be > 0, got %d", sequence.ErrInvalidConfig, by)
	}
	if s.db == nil {
		return 0, fmt.Errorf("%w: store must not be bound to a nil DBTX", sequence.ErrInvalidConfig)
	}
	if s.requireTx && !s.inTx {
		return 0, sequence.ErrNoTransaction
	}
	if !s.dialect.returning() {
		res, err := s.db.ExecContext(ctx, s.query, s.dialect.incrArgs(k.Name, k.Scope, period, by)...)
		if err != nil {
			return 0, s.mapError(ctx, k, period, err)
		}
		counter, err := res.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("sqlstore: incr: %w", err)
		}
		return counter, nil
	}
	var counter int64
	if err := s.db.QueryRowContext(ctx, s.query, s.dialect.incrArgs(k.Name, k.Scope, period, by)...).Scan(&counter); err != nil {
		return 0, s.mapError(ctx, k, period, err)
	}
	return counter, nil
}

// Current implements sequence.Resetter. It is a plain read and is allowed on a pool even with
// RequireTx.
func (s *Store) Current(ctx context.Context, k sequence.Key, period string) (int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if s.db == nil {
		return 0, false, fmt.Errorf("%w: store must not be bound to a nil DBTX", sequence.ErrInvalidConfig)
	}
	var counter int64
	err := s.db.QueryRowContext(ctx, s.current, k.Name, k.Scope, period).Scan(&counter)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, s.mapError(ctx, k, period, err)
	}
	return counter, true, nil
}

// Set implements sequence.Resetter with one atomic upsert of an absolute value.
func (s *Store) Set(ctx context.Context, k sequence.Key, period string, raw int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if raw < 0 {
		return fmt.Errorf("%w: raw count must be >= 0, got %d", sequence.ErrInvalidConfig, raw)
	}
	if s.db == nil {
		return fmt.Errorf("%w: store must not be bound to a nil DBTX", sequence.ErrInvalidConfig)
	}
	if s.requireTx && !s.inTx {
		return sequence.ErrNoTransaction
	}
	if _, err := s.db.ExecContext(ctx, s.set, s.dialect.setArgs(k.Name, k.Scope, period, raw)...); err != nil {
		return s.mapError(ctx, k, period, err)
	}
	return nil
}
