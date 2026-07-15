//go:build linux

package namecapture

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestLiveCollectorObservesTLSName(t *testing.T) {
	interfaceName := os.Getenv("FLOWLENS_CAPTURE_INTERFACE")
	if interfaceName == "" {
		t.Skip("FLOWLENS_CAPTURE_INTERFACE is not configured")
	}
	collector, err := NewCollector([]string{interfaceName})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, collector.Close()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	evidence := make(chan model.NameEvidence, 32)
	errorsChannel := make(chan error, 1)
	go func() { errorsChannel <- collector.Run(ctx, evidence) }()

	connection, err := tls.Dial("tcp", "example.com:443", &tls.Config{ServerName: "example.com", MinVersion: tls.VersionTLS12})
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case item := <-evidence:
			if item.Source == "tls_sni" && item.Name == "example.com" {
				cancel()
				return
			}
		case err := <-errorsChannel:
			require.NoError(t, err)
		case <-deadline.C:
			require.FailNow(t, "timed out waiting for TLS SNI evidence")
		}
	}
}
