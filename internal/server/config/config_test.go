package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadAppliesServerDefaults(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_DATABASE_URL":    "postgres://flowlens:secret@postgres/flowlens",
		"FLOWLENS_AGENT_TOKEN":     "agent-secret",
		"FLOWLENS_BOOTSTRAP_TOKEN": "bootstrap-secret",
		"FLOWLENS_SECRET_KEY":      "3031323334353637383961626364656630313233343536373839616263646566",
		"FLOWLENS_PUBLIC_URL":      "https://flowlens.example",
	}

	loaded, err := Load(func(key string) string { return values[key] })

	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8088", loaded.ListenAddress)
	require.Equal(t, "/opt/flowlens/web", loaded.WebDir)
	require.Equal(t, "/var/lib/flowlens/geoip/country.mmdb", loaded.GeoIPCountryPath)
	require.Equal(t, "/var/lib/flowlens/geoip/asn.mmdb", loaded.GeoIPASNPath)
	require.Equal(t, 10*time.Second, loaded.ShutdownTimeout)
	require.Equal(t, int64(10<<30), loaded.DatabaseBudgetBytes)
	require.Equal(t, "bootstrap-secret", loaded.BootstrapToken)
	require.Len(t, loaded.SecretKey, 32)
	require.Equal(t, "https://flowlens.example", loaded.PublicURL)
	require.False(t, loaded.WebhookAllowHTTP)
}

func TestLoadValidatesDatabaseBudget(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_DATABASE_URL": "postgres://flowlens:secret@postgres/flowlens", "FLOWLENS_AGENT_TOKEN": "agent-secret",
		"FLOWLENS_BOOTSTRAP_TOKEN": "bootstrap-secret", "FLOWLENS_SECRET_KEY": "3031323334353637383961626364656630313233343536373839616263646566",
		"FLOWLENS_PUBLIC_URL": "https://flowlens.example", "FLOWLENS_DATABASE_BUDGET_BYTES": "21474836480",
	}
	loaded, err := Load(func(key string) string { return values[key] })
	require.NoError(t, err)
	require.Equal(t, int64(20<<30), loaded.DatabaseBudgetBytes)
	values["FLOWLENS_DATABASE_BUDGET_BYTES"] = "0"
	_, err = Load(func(key string) string { return values[key] })
	require.ErrorContains(t, err, "FLOWLENS_DATABASE_BUDGET_BYTES")
}

func TestLoadRejectsInvalidSecretKey(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_DATABASE_URL":    "postgres://flowlens:secret@postgres/flowlens",
		"FLOWLENS_AGENT_TOKEN":     "agent-secret",
		"FLOWLENS_BOOTSTRAP_TOKEN": "bootstrap-secret",
		"FLOWLENS_SECRET_KEY":      "too-short",
		"FLOWLENS_PUBLIC_URL":      "https://flowlens.example",
	}
	_, err := Load(func(key string) string { return values[key] })
	require.ErrorContains(t, err, "FLOWLENS_SECRET_KEY")
}

func TestLoadRejectsMissingBootstrapTokenWithoutLeakingOtherSecrets(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_DATABASE_URL": "postgres://flowlens:database-secret@postgres/flowlens",
		"FLOWLENS_AGENT_TOKEN":  "agent-secret",
	}

	_, err := Load(func(key string) string { return values[key] })

	require.ErrorContains(t, err, "FLOWLENS_BOOTSTRAP_TOKEN")
	require.NotContains(t, err.Error(), "database-secret")
	require.NotContains(t, err.Error(), "agent-secret")
}

func TestLoadRejectsMissingDatabaseWithoutLeakingToken(t *testing.T) {
	values := map[string]string{"FLOWLENS_AGENT_TOKEN": "do-not-print-this"}

	_, err := Load(func(key string) string { return values[key] })

	require.ErrorContains(t, err, "FLOWLENS_DATABASE_URL")
	require.NotContains(t, err.Error(), "do-not-print-this")
}
