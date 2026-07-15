package alerts

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

func EvaluateRule(rule Rule, observation Observation, now time.Time) []Finding {
	if !rule.Enabled || observation.NodeID == "" {
		return nil
	}
	observed, comparison, triggered := values(rule, observation)
	if !triggered {
		return nil
	}
	window := rule.WindowSeconds
	if window <= 0 {
		window = 300
	}
	var ownerID *string
	if observation.OwnerID != "" {
		owner := observation.OwnerID
		ownerID = &owner
	}
	evidence := map[string]string{"kind": string(rule.Kind), "node_id": observation.NodeID}
	if observation.OwnerID != "" {
		evidence["owner_id"] = observation.OwnerID
	}
	if observation.Destination != "" {
		evidence["destination"] = observation.Destination
	}
	if observation.Collector != "" {
		evidence["collector"] = observation.Collector
	}
	return []Finding{{
		RuleID: rule.ID, Fingerprint: Fingerprint(rule.ID, observation.NodeID, observation.OwnerID, observation.Destination, observation.Collector),
		Title: rule.Name, Severity: rule.Severity, NodeID: observation.NodeID, OwnerID: ownerID, StartedAt: now,
		ObservedValue: observed, ComparisonValue: &comparison, WindowSeconds: window, Evidence: evidence,
	}}
}

func values(rule Rule, observation Observation) (observed, comparison float64, triggered bool) {
	switch rule.Kind {
	case KindAbsoluteRate:
		return observation.RateBPS, rule.Threshold, observation.RateBPS > rule.Threshold
	case KindOwnerBaseline:
		comparison = observation.OwnerBaselineBytes * rule.Multiplier
		return observation.OwnerBytes, comparison, observation.OwnerBaselineBytes > 0 && observation.OwnerBytes > comparison
	case KindNewDestination:
		return boolNumber(observation.DestinationIsNew), 0, observation.DestinationIsNew
	case KindDomainCoverage:
		return observation.DomainCoverage, rule.Threshold, observation.DomainCoverage < rule.Threshold
	case KindAgentStale:
		return observation.AgentAgeSeconds, rule.Threshold, observation.AgentAgeSeconds > rule.Threshold
	case KindCollectorUnhealthy:
		return boolNumber(!observation.CollectorHealthy), 0, !observation.CollectorHealthy
	case KindBufferPressure:
		return observation.BufferUsagePercent, rule.Threshold, observation.BufferUsagePercent > rule.Threshold
	case KindDatabasePressure:
		return observation.DatabaseUsagePercent, rule.Threshold, observation.DatabaseUsagePercent > rule.Threshold
	case KindWebhookFailures:
		return observation.WebhookTerminalFailures, rule.Threshold, observation.WebhookTerminalFailures >= rule.Threshold
	default:
		return 0, 0, false
	}
}

func Fingerprint(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(strconv.Itoa(len(part))))
		hash.Write([]byte{':'})
		hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func boolNumber(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
