package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOperationsMigrationDefinesSingleAdminAndHashedSessions(t *testing.T) {
	schema, err := files.ReadFile("003_operations.sql")
	require.NoError(t, err)
	contents := string(schema)
	for _, expected := range []string{
		"CREATE TABLE administrators",
		"CHECK (id = 1)",
		"password_hash",
		"CREATE TABLE browser_sessions",
		"token_hash bytea PRIMARY KEY",
		"idle_expires_at",
		"expires_at",
	} {
		require.True(t, strings.Contains(contents, expected), expected)
	}
}

