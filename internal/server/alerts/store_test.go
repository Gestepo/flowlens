package alerts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAlertStateTransitionsOpenUpdateResolveAndRecur(t *testing.T) {
	now := time.Date(2026, 7, 15, 4, 0, 0, 0, time.UTC)
	finding := Finding{Fingerprint: "fingerprint", Severity: SeverityWarning, StartedAt: now}

	state, notify := ApplyFinding(nil, finding, now)
	require.True(t, notify)
	require.Equal(t, StatusOpen, state.Status)
	require.Equal(t, int64(1), state.OccurrenceCount)

	state, notify = ApplyFinding(&state, finding, now.Add(time.Minute))
	require.False(t, notify)
	require.Equal(t, int64(2), state.OccurrenceCount)
	require.Equal(t, now.Add(time.Minute), state.LastSeenAt)

	state, resolved := ApplyMissing(state, now.Add(2*time.Minute))
	require.False(t, resolved)
	require.Equal(t, 1, state.MissingWindows)
	state, resolved = ApplyMissing(state, now.Add(3*time.Minute))
	require.True(t, resolved)
	require.Equal(t, StatusResolved, state.Status)
	require.NotNil(t, state.ResolvedAt)

	recurrence, notify := ApplyFinding(nil, finding, now.Add(4*time.Minute))
	require.True(t, notify)
	require.Equal(t, int64(1), recurrence.OccurrenceCount)
	require.Equal(t, now.Add(4*time.Minute), recurrence.FirstSeenAt)
}

func TestSeverityIncreaseRequestsDelivery(t *testing.T) {
	now := time.Date(2026, 7, 15, 4, 0, 0, 0, time.UTC)
	state := AlertState{Status: StatusOpen, Severity: SeverityWarning, OccurrenceCount: 2, FirstSeenAt: now, LastSeenAt: now}
	finding := Finding{Severity: SeverityCritical, StartedAt: now.Add(time.Minute)}
	updated, notify := ApplyFinding(&state, finding, finding.StartedAt)
	require.True(t, notify)
	require.Equal(t, SeverityCritical, updated.Severity)
}

func TestEvidenceRejectsUnknownKeysAndOversizedValues(t *testing.T) {
	require.Error(t, ValidateEvidence(map[string]string{"secret": "must-not-persist"}))
	require.Error(t, ValidateEvidence(map[string]string{"destination": string(make([]byte, 17<<10))}))
	require.NoError(t, ValidateEvidence(map[string]string{"kind": "new_destination", "node_id": "node", "owner_id": "owner", "destination": "example.com"}))
}
