package partition

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type Service struct{ database Executor }

func NewService(database Executor) *Service { return &Service{database: database} }

func (service *Service) Ensure(ctx context.Context, at time.Time) error {
	current := at.UTC().Truncate(24 * time.Hour)
	current = time.Date(current.Year(), current.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, parent := range []string{"interface_deltas", "connection_details", "proxy_request_details"} {
		for offset := 0; offset < 3; offset++ {
			start := current.AddDate(0, offset, 0)
			end := start.AddDate(0, 1, 0)
			name := fmt.Sprintf("%s_%s", parent, start.Format("200601"))
			statement := fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%s) TO (%s)",
				pgx.Identifier{name}.Sanitize(), pgx.Identifier{parent}.Sanitize(), quoteTimestamp(start), quoteTimestamp(end),
			)
			if _, err := service.database.Exec(ctx, statement); err != nil {
				return fmt.Errorf("ensure partition %s: %w", name, err)
			}
		}
	}
	return nil
}

func quoteTimestamp(value time.Time) string {
	return fmt.Sprintf("'%s'", value.UTC().Format(time.RFC3339))
}
