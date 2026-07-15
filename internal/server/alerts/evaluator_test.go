package alerts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvaluateRuleCoversInitialAlertKinds(t *testing.T) {
	now := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		rule        Rule
		observation Observation
		wantValue   float64
		wantCompare float64
	}{
		{"absolute rate", Rule{ID: "rate", Kind: KindAbsoluteRate, Threshold: 10_000_000}, Observation{RateBPS: 12_000_000}, 12_000_000, 10_000_000},
		{"owner baseline", Rule{ID: "baseline", Kind: KindOwnerBaseline, Multiplier: 4}, Observation{OwnerID: "container:web", OwnerBytes: 5000, OwnerBaselineBytes: 1000}, 5000, 4000},
		{"new destination", Rule{ID: "destination", Kind: KindNewDestination}, Observation{OwnerID: "process:curl", Destination: "api.example.test", DestinationIsNew: true}, 1, 0},
		{"domain coverage", Rule{ID: "coverage", Kind: KindDomainCoverage, Threshold: 60}, Observation{DomainCoverage: 42}, 42, 60},
		{"stale agent", Rule{ID: "stale", Kind: KindAgentStale, Threshold: 60}, Observation{AgentAgeSeconds: 75}, 75, 60},
		{"collector", Rule{ID: "collector", Kind: KindCollectorUnhealthy}, Observation{Collector: "npm", CollectorHealthy: false}, 1, 0},
		{"buffer", Rule{ID: "buffer", Kind: KindBufferPressure, Threshold: 80}, Observation{BufferUsagePercent: 91}, 91, 80},
		{"database", Rule{ID: "database", Kind: KindDatabasePressure, Threshold: 80}, Observation{DatabaseUsagePercent: 92}, 92, 80},
		{"webhook", Rule{ID: "webhook", Kind: KindWebhookFailures, Threshold: 3}, Observation{WebhookTerminalFailures: 4}, 4, 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.rule.Name, test.rule.Severity, test.rule.Enabled = test.name, SeverityWarning, true
			test.observation.NodeID = "flowlens-node-1"
			findings := EvaluateRule(test.rule, test.observation, now)
			require.Len(t, findings, 1)
			finding := findings[0]
			require.Equal(t, test.wantValue, finding.ObservedValue)
			require.NotNil(t, finding.ComparisonValue)
			require.Equal(t, test.wantCompare, *finding.ComparisonValue)
			require.Equal(t, "flowlens-node-1", finding.NodeID)
			require.Equal(t, 300, finding.WindowSeconds)
			require.NotEmpty(t, finding.Fingerprint)
			require.NotEmpty(t, finding.Evidence)
		})
	}
}

func TestConservativeDefaultsDoNotTriggerOnOrdinaryTraffic(t *testing.T) {
	now := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	ordinary := Observation{
		NodeID: "flowlens-node-1", RateBPS: 2_000_000, OwnerBytes: 1200, OwnerBaselineBytes: 1000,
		DomainCoverage: 88, AgentAgeSeconds: 5, CollectorHealthy: true, BufferUsagePercent: 20, DatabaseUsagePercent: 20,
	}
	for _, rule := range DefaultRules() {
		require.Empty(t, EvaluateRule(rule, ordinary, now), rule.ID)
	}
}

func TestFingerprintDoesNotContainOwnerOrDestination(t *testing.T) {
	fingerprint := Fingerprint("rule", "node", "secret-owner", "private.example.test")
	require.NotContains(t, fingerprint, "secret-owner")
	require.NotContains(t, fingerprint, "private.example.test")
	require.Len(t, fingerprint, 64)
}

func TestCollectorFindingsUseDistinctFingerprints(t *testing.T) {
	rule := Rule{ID: "collector", Kind: KindCollectorUnhealthy, Name: "采集器异常", Enabled: true, Severity: SeverityWarning}
	one := EvaluateRule(rule, Observation{NodeID: "node", Collector: "npm", CollectorHealthy: false}, time.Now())[0]
	two := EvaluateRule(rule, Observation{NodeID: "node", Collector: "docker", CollectorHealthy: false}, time.Now())[0]
	require.NotEqual(t, one.Fingerprint, two.Fingerprint)
}
