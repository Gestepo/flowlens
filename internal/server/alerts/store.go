package alerts

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	StatusOpen     = "open"
	StatusResolved = "resolved"
)

type AlertState struct {
	Status          string
	Severity        string
	OccurrenceCount int64
	MissingWindows  int
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
	ResolvedAt      *time.Time
}

func ApplyFinding(current *AlertState, finding Finding, now time.Time) (AlertState, bool) {
	if current == nil || current.Status != StatusOpen {
		return AlertState{
			Status: StatusOpen, Severity: finding.Severity, OccurrenceCount: 1,
			FirstSeenAt: now, LastSeenAt: now,
		}, true
	}
	next := *current
	notify := severityRank(finding.Severity) > severityRank(current.Severity)
	if notify {
		next.Severity = finding.Severity
	}
	next.OccurrenceCount++
	next.MissingWindows = 0
	next.LastSeenAt = now
	next.ResolvedAt = nil
	return next, notify
}

func ApplyMissing(current AlertState, now time.Time) (AlertState, bool) {
	if current.Status != StatusOpen {
		return current, false
	}
	current.MissingWindows++
	if current.MissingWindows < 2 {
		return current, false
	}
	current.Status = StatusResolved
	current.ResolvedAt = &now
	return current, true
}

func ValidateEvidence(evidence map[string]string) error {
	allowed := map[string]bool{"kind": true, "node_id": true, "owner_id": true, "destination": true, "collector": true}
	for key := range evidence {
		if !allowed[key] {
			return errors.New("alert evidence contains an unsupported key")
		}
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return errors.New("alert evidence is invalid")
	}
	if len(encoded) > 16<<10 {
		return errors.New("alert evidence exceeds 16 KiB")
	}
	return nil
}

func severityRank(severity string) int {
	switch severity {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}
