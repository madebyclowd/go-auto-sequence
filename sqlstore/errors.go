package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

// kind is what the store understands about a driver error.
type kind int

const (
	other    kind = iota
	lockWait      // the server gave up waiting for a lock
	overflow      // the counter would exceed the column's range
)

// mapError turns driver errors into the library's sentinels without importing a driver. See
// classify for how an error is recognised; an error that is not recognised is wrapped unchanged, so
// nothing is ever lost.
func (s *Store) mapError(ctx context.Context, k sequence.Key, period string, err error) error {
	switch s.dialect.classify(err) {
	case lockWait:
		return fmt.Errorf("%w: %w", sequence.ErrLockTimeout, err)
	case overflow:
		return &sequence.ExhaustedError{Key: k, Period: period, Max: math.MaxInt64, Seq: math.MaxInt64}
	}
	if s.dialect == Postgres && postgresQueryCanceled(err) {
		// A deadline that expired while the server waited on a row lock: report both causes.
		if cerr := ctx.Err(); cerr != nil {
			return fmt.Errorf("%w: %w: %w", sequence.ErrLockTimeout, cerr, err)
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("sqlstore: incr: %w", err)
}

var mysqlPrefix = regexp.MustCompile(`^Error (\d+) \(([0-9A-Za-z]{5})\)`)

// driverCode extracts a numeric code and an SQLSTATE from a driver error without importing the
// driver. Three tiers, most structural first:
//  1. a SQLState() string method (pgx and lib/pq),
//  2. a Code() int method (modernc.org/sqlite),
//  3. the stable text prefix "Error NNNN (SQLSTATE)" of go-sql-driver/mysql, or the SQLite
//     message text of drivers that expose neither method.
func driverCode(err error) (code int, sqlState string) {
	var st interface{ SQLState() string }
	if errors.As(err, &st) {
		sqlState = st.SQLState()
	}
	var c interface{ Code() int }
	if errors.As(err, &c) {
		code = c.Code()
	}
	if code == 0 && sqlState == "" {
		if m := mysqlPrefix.FindStringSubmatch(err.Error()); m != nil {
			code, _ = strconv.Atoi(m[1])
			sqlState = m[2]
		}
	}
	return code, sqlState
}

func (d Dialect) classify(err error) kind {
	code, sqlState := driverCode(err)
	switch d {
	case Postgres:
		switch sqlState {
		case "55P03": // lock_not_available
			return lockWait
		case "22003": // numeric_value_out_of_range
			return overflow
		}
	case MySQL:
		switch code {
		case 1205: // ER_LOCK_WAIT_TIMEOUT
			return lockWait
		case 1690, 1264: // ER_DATA_OUT_OF_RANGE, ER_WARN_DATA_OUT_OF_RANGE
			return overflow
		}
	case SQLite:
		if code == 0 { // a driver that exposes no code: fall back to the message text
			switch {
			case sqliteBusyText.MatchString(err.Error()):
				return lockWait
			case sqliteCheckFailed(err):
				return overflow
			}
		}
		switch code & 0xff { // strip the extended result code
		case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED (after the busy timeout)
			return lockWait
		case 19: // SQLITE_CONSTRAINT: only our CHECK can fail, because an overflow makes counter a REAL
			if sqliteCheckFailed(err) {
				return overflow
			}
		}
	}
	return other
}

func postgresQueryCanceled(err error) bool {
	_, st := driverCode(err)
	return st == "57014" // query_canceled
}

var (
	sqliteBusyText  = regexp.MustCompile(`database is locked|SQLITE_BUSY|SQLITE_LOCKED`)
	sqliteCheckText = regexp.MustCompile(`CHECK constraint failed|SQLITE_CONSTRAINT_CHECK`)
)

func sqliteCheckFailed(err error) bool { return sqliteCheckText.MatchString(err.Error()) }
