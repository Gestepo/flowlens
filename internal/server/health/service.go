package health

import "time"

const (
	StatusHealthy = "healthy"
	StatusDelayed = "delayed"
	StatusOffline = "offline"
	StatusPartial = "partial"
	StatusWarning = "warning"
)

type Snapshot struct {
	LastSeenAt              time.Time
	FailedCollectors        []string
	DatabaseUsagePercent    float64
	BufferUsagePercent      float64
	WebhookTerminalFailures int
}

type State struct {
	Status                  string    `json:"status"`
	LastSeenAt              time.Time `json:"last_seen_at"`
	FailedCollectors        []string  `json:"failed_collectors"`
	DatabaseUsagePercent    float64   `json:"database_usage_percent"`
	BufferUsagePercent      float64   `json:"buffer_usage_percent"`
	WebhookTerminalFailures int       `json:"webhook_terminal_failures"`
}

func Classify(now time.Time, snapshot Snapshot) State {
	status := StatusHealthy
	age := now.Sub(snapshot.LastSeenAt)
	if snapshot.LastSeenAt.IsZero() || age > 60*time.Second {
		status = StatusOffline
	} else if age >= 15*time.Second {
		status = StatusDelayed
	} else if len(snapshot.FailedCollectors) > 0 {
		status = StatusPartial
	} else if snapshot.DatabaseUsagePercent > 80 || snapshot.BufferUsagePercent > 80 {
		status = StatusWarning
	}
	return State{
		Status: status, LastSeenAt: snapshot.LastSeenAt, FailedCollectors: snapshot.FailedCollectors,
		DatabaseUsagePercent: snapshot.DatabaseUsagePercent, BufferUsagePercent: snapshot.BufferUsagePercent,
		WebhookTerminalFailures: snapshot.WebhookTerminalFailures,
	}
}
