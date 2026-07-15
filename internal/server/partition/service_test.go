package partition

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestEnsureCreatesCurrentAndTwoFutureMonthlyPartitions(t *testing.T) {
	recorder := &execRecorder{}
	service := NewService(recorder)
	require.NoError(t, service.Ensure(context.Background(), time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)))
	require.Len(t, recorder.statements, 9)
	for _, expected := range []string{
		"interface_deltas_202607", "interface_deltas_202608", "interface_deltas_202609",
		"connection_details_202607", "connection_details_202608", "connection_details_202609",
		"proxy_request_details_202607", "proxy_request_details_202608", "proxy_request_details_202609",
		"FROM ('2026-07-01T00:00:00Z') TO ('2026-08-01T00:00:00Z')",
	} {
		require.True(t, containsStatement(recorder.statements, expected), expected)
	}
}

type execRecorder struct{ statements []string }

func (recorder *execRecorder) Exec(_ context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	recorder.statements = append(recorder.statements, statement)
	return pgconn.CommandTag{}, nil
}

func containsStatement(statements []string, expected string) bool {
	for _, statement := range statements {
		if strings.Contains(statement, expected) {
			return true
		}
	}
	return false
}
