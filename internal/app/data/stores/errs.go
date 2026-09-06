package stores

import (
	"database/sql"
	"errors"
	"fmt"
)

// NotFoundError is what a store returns when a query addressed exactly one
// row and found none. It wraps sql.ErrNoRows, so a caller that still matches
// on that keeps working while call sites move onto IsNotFound.
type NotFoundError struct {
	Entity string
	Key    string
	err    error
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("%s %s: not found", e.Entity, e.Key)
}

func (e NotFoundError) Unwrap() error { return e.err }

// IsNotFound reports whether err is a NotFoundError, however deeply wrapped.
func IsNotFound(err error) bool {
	var notFound NotFoundError
	return errors.As(err, &notFound)
}

// ErrStale means the row changed after the caller read its revision. A
// revision-guarded UPDATE that matches nothing is a stale read, not a missing
// row, so it gets its own sentinel rather than folding into NotFoundError:
// IsNotFound(err) must stay false for it, or a caller that retries on
// conflict starts silently reporting the item as deleted instead.
var ErrStale = errors.New("stale revision")

// errTransformQueryOne turns sql.ErrNoRows from a single-row query into a
// NotFoundError carrying entity and key for the message. Any other error
// passes through unchanged. Never call this on a revision-guarded UPDATE —
// see ErrStale.
func errTransformQueryOne(entity, key string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return NotFoundError{Entity: entity, Key: key, err: err}
	}
	return err
}

// errTransformQueryMany swallows sql.ErrNoRows from a multi-row query: no
// rows is an empty result, not a failure. Any other error passes through
// unchanged.
func errTransformQueryMany(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// wrap prefixes err with msg, the same convention the old queries package's
// wrap() used. nil in, nil out.
func wrap(msg string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// null converts a plain string into the sql.NullString a generated params
// struct expects, valid whenever the value is non-empty.
func null(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }
