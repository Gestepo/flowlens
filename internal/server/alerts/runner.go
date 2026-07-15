package alerts

import (
	"context"
	"time"
)

type ObservationSource interface {
	Rules(context.Context) ([]Rule, error)
	Nodes(context.Context) ([]string, error)
	Observations(context.Context, Rule, string, time.Time) ([]Observation, error)
}

type Reconciler interface {
	Reconcile(context.Context, Rule, string, []Finding, time.Time) error
}

type Runner struct {
	source     ObservationSource
	reconciler Reconciler
}

func NewRunner(source ObservationSource, reconciler Reconciler) *Runner {
	return &Runner{source: source, reconciler: reconciler}
}

func (runner *Runner) Run(ctx context.Context, now time.Time) error {
	rules, err := runner.source.Rules(ctx)
	if err != nil {
		return err
	}
	nodes, err := runner.source.Nodes(ctx)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		for _, nodeID := range nodes {
			observations, err := runner.source.Observations(ctx, rule, nodeID, now)
			if err != nil {
				return err
			}
			var findings []Finding
			for _, observation := range observations {
				findings = append(findings, EvaluateRule(rule, observation, now)...)
			}
			if err := runner.reconciler.Reconcile(ctx, rule, nodeID, findings, now); err != nil {
				return err
			}
		}
	}
	return nil
}
