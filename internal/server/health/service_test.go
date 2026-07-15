package health

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassifyNodeHealthStates(t *testing.T) {
	now := time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{"healthy", Snapshot{LastSeenAt: now.Add(-14 * time.Second)}, StatusHealthy},
		{"delayed", Snapshot{LastSeenAt: now.Add(-15 * time.Second)}, StatusDelayed},
		{"offline", Snapshot{LastSeenAt: now.Add(-61 * time.Second)}, StatusOffline},
		{"collector partial", Snapshot{LastSeenAt: now.Add(-time.Second), FailedCollectors: []string{"npm"}}, StatusPartial},
		{"database pressure", Snapshot{LastSeenAt: now.Add(-time.Second), DatabaseUsagePercent: 81}, StatusWarning},
		{"buffer pressure", Snapshot{LastSeenAt: now.Add(-time.Second), BufferUsagePercent: 81}, StatusWarning},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { require.Equal(t, test.want, Classify(now, test.snapshot).Status) })
	}
}

func TestWebhookFailuresRemainVisibleWithoutChangingIngestionHealth(t *testing.T) {
	now := time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC)
	state := Classify(now, Snapshot{LastSeenAt: now.Add(-time.Second), WebhookTerminalFailures: 3})
	require.Equal(t, StatusHealthy, state.Status)
	require.Equal(t, 3, state.WebhookTerminalFailures)
}
