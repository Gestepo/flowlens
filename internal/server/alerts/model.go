package alerts

import "time"

type Kind string

const (
	KindAbsoluteRate       Kind = "absolute_rate"
	KindOwnerBaseline      Kind = "owner_baseline"
	KindNewDestination     Kind = "new_destination"
	KindDomainCoverage     Kind = "domain_coverage"
	KindAgentStale         Kind = "agent_stale"
	KindCollectorUnhealthy Kind = "collector_unhealthy"
	KindBufferPressure     Kind = "buffer_pressure"
	KindDatabasePressure   Kind = "database_pressure"
	KindWebhookFailures    Kind = "webhook_failures"
)

const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

type Rule struct {
	ID            string
	Kind          Kind
	Name          string
	Enabled       bool
	Severity      string
	Threshold     float64
	Multiplier    float64
	WindowSeconds int
}

type Observation struct {
	NodeID                  string
	RateBPS                 float64
	OwnerID                 string
	OwnerBytes              float64
	OwnerBaselineBytes      float64
	Destination             string
	DestinationIsNew        bool
	DomainCoverage          float64
	AgentAgeSeconds         float64
	Collector               string
	CollectorHealthy        bool
	BufferUsagePercent      float64
	DatabaseUsagePercent    float64
	WebhookTerminalFailures float64
}

type Finding struct {
	RuleID          string
	Fingerprint     string
	Title           string
	Severity        string
	NodeID          string
	OwnerID         *string
	StartedAt       time.Time
	ObservedValue   float64
	ComparisonValue *float64
	WindowSeconds   int
	Evidence        map[string]string
}

func DefaultRules() []Rule {
	return []Rule{
		{ID: "absolute-rate", Kind: KindAbsoluteRate, Name: "传输速率过高", Enabled: true, Severity: SeverityWarning, Threshold: 100_000_000, WindowSeconds: 300},
		{ID: "owner-baseline", Kind: KindOwnerBaseline, Name: "所有者流量偏离基线", Enabled: true, Severity: SeverityWarning, Multiplier: 5, WindowSeconds: 300},
		{ID: "new-destination", Kind: KindNewDestination, Name: "发现新的远程目标", Enabled: true, Severity: SeverityInfo, WindowSeconds: 300},
		{ID: "domain-coverage", Kind: KindDomainCoverage, Name: "域名识别率过低", Enabled: true, Severity: SeverityWarning, Threshold: 50, WindowSeconds: 300},
		{ID: "agent-stale", Kind: KindAgentStale, Name: "采集节点离线", Enabled: true, Severity: SeverityCritical, Threshold: 60, WindowSeconds: 300},
		{ID: "collector-unhealthy", Kind: KindCollectorUnhealthy, Name: "采集器异常", Enabled: true, Severity: SeverityWarning, WindowSeconds: 300},
		{ID: "buffer-pressure", Kind: KindBufferPressure, Name: "Agent 缓冲区压力", Enabled: true, Severity: SeverityWarning, Threshold: 80, WindowSeconds: 300},
		{ID: "database-pressure", Kind: KindDatabasePressure, Name: "数据库容量压力", Enabled: true, Severity: SeverityWarning, Threshold: 80, WindowSeconds: 300},
		{ID: "webhook-failures", Kind: KindWebhookFailures, Name: "Webhook 连续失败", Enabled: true, Severity: SeverityWarning, Threshold: 3, WindowSeconds: 300},
	}
}
