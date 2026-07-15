package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunnerEvaluatesEveryRuleAndNodeThenReconcilesOnce(t *testing.T) {
	now := time.Date(2026, 7, 15, 15, 0, 0, 0, time.UTC)
	rule := Rule{ID: "rate", Kind: KindAbsoluteRate, Name: "速率", Enabled: true, Severity: SeverityWarning, Threshold: 10, WindowSeconds: 300}
	source := &runnerSource{rules: []Rule{rule}, nodes: []string{"node-a"}, observations: []Observation{{NodeID: "node-a", RateBPS: 20}, {NodeID: "node-a", RateBPS: 5}}}
	reconciler := &runnerReconciler{}
	runner := NewRunner(source, reconciler)
	require.NoError(t, runner.Run(context.Background(), now))
	require.Equal(t, 1, reconciler.calls)
	require.Len(t, reconciler.findings, 1)
	require.Equal(t, float64(20), reconciler.findings[0].ObservedValue)
}

type runnerSource struct {
	rules        []Rule
	nodes        []string
	observations []Observation
}

func (source *runnerSource) Rules(context.Context) ([]Rule, error)   { return source.rules, nil }
func (source *runnerSource) Nodes(context.Context) ([]string, error) { return source.nodes, nil }
func (source *runnerSource) Observations(context.Context, Rule, string, time.Time) ([]Observation, error) {
	return source.observations, nil
}

type runnerReconciler struct {
	calls    int
	findings []Finding
}

func (reconciler *runnerReconciler) Reconcile(_ context.Context, _ Rule, _ string, findings []Finding, _ time.Time) error {
	reconciler.calls++
	reconciler.findings = findings
	return nil
}
