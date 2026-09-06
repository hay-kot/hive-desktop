package store

import (
	"cmp"
	"context"
	"database/sql"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/hay-kot/hive-desktop/internal/app/observe"
)

var tracer = observe.Tracer("/internal/app/store")

// tracingDBTX is the sqlc querier with a span around every statement. It is
// installed wherever a *Queries is built — the pool, a bound transaction, and
// WithTx — because sqlc's own WithTx rebuilds the struct around a raw *sql.Tx
// and would drop the wrapper.
//
// Spans are conditional (see [observe.StartConditionalSpan]): this database is
// busy with background work no request asked for, and a root span per poll-loop
// statement would bury the traces worth reading.
type tracingDBTX struct{ db DBTX }

func newTracingDBTX(db DBTX) DBTX { return tracingDBTX{db: db} }

func (t tracingDBTX) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	ctx, span := observe.StartConditionalSpan(ctx, tracer, cmp.Or(sqlcName(query), "db.exec"))
	defer span.End()

	result, err := t.db.ExecContext(ctx, query, args...)
	if err != nil {
		observe.RecordError(span, err)
		return result, err
	}
	if affected, err := result.RowsAffected(); err == nil {
		span.SetAttributes(attribute.Int64("db.rows_affected", affected))
	}
	return result, nil
}

func (t tracingDBTX) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	ctx, span := observe.StartConditionalSpan(ctx, tracer, cmp.Or(sqlcName(query), "db.query"))
	defer span.End()

	rows, err := t.db.QueryContext(ctx, query, args...) //nolint:sqlclosecheck // pass-through: the sqlc caller owns the rows
	if err != nil {
		observe.RecordError(span, err)
	}
	return rows, err
}

// QueryRowContext's span covers the statement, not the scan: *sql.Row defers
// both the error and the row until Scan, which happens after this returns.
func (t tracingDBTX) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	ctx, span := observe.StartConditionalSpan(ctx, tracer, cmp.Or(sqlcName(query), "db.query_row"))
	defer span.End()

	return t.db.QueryRowContext(ctx, query, args...)
}

func (t tracingDBTX) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	ctx, span := observe.StartConditionalSpan(ctx, tracer, cmp.Or(sqlcName(query), "db.prepare"))
	defer span.End()

	stmt, err := t.db.PrepareContext(ctx, query) //nolint:sqlclosecheck // pass-through: the sqlc caller owns the statement
	if err != nil {
		observe.RecordError(span, err)
	}
	return stmt, err
}

// sqlcName reads the query name out of the header comment sqlc emits into every
// generated statement ("-- name: AppendEvent :one"). Hand-written SQL in this
// package carries no such header and falls back to the statement kind.
func sqlcName(query string) string {
	for line := range strings.SplitSeq(query, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-- name:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return ""
		}
		return "db." + fields[2]
	}
	return ""
}
