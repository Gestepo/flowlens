package attribution

import (
	"net/netip"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestDecideAtUsesDirectThenStrongestMostRecentEvidence(t *testing.T) {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	connection := connectionFixture()
	connection.Remote.Domain = "direct.example"
	connection.Remote.Confidence = model.ConfidenceConfirmed
	decision := DecideAt(connection, nil, now)
	require.Equal(t, "direct.example", decision.DisplayName)
	require.Equal(t, model.ConfidenceConfirmed, decision.Confidence)
	require.Equal(t, "connection", decision.EvidenceSource)

	connection.Remote.Domain = ""
	connection.Remote.Confidence = model.ConfidenceIPOnly
	evidence := []model.NameEvidence{
		{IP: connection.Remote.IP, Name: "old.example", Source: "dns", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Minute)},
		{IP: connection.Remote.IP, Name: "new.example", Source: "dns", ValidFrom: now.Add(-time.Second), ValidUntil: now.Add(time.Minute)},
		{IP: connection.Remote.IP, Name: "tls.example", Source: "tls_sni", ValidFrom: now.Add(-2 * time.Minute), ValidUntil: now.Add(time.Minute)},
	}
	decision = DecideAt(connection, evidence, now)
	require.Equal(t, "tls.example", decision.DisplayName)
	require.Equal(t, model.ConfidenceConfirmed, decision.Confidence)
	require.Equal(t, "tls_sni", decision.EvidenceSource)

	decision = DecideAt(connection, evidence[:2], now)
	require.Equal(t, "new.example", decision.DisplayName)
	require.Equal(t, model.ConfidenceInferred, decision.Confidence)
}

func TestDecideAtReturnsIPOnlyForExpiredOrMismatchedEvidence(t *testing.T) {
	now := time.Now().UTC()
	connection := connectionFixture()
	decision := DecideAt(connection, []model.NameEvidence{
		{IP: connection.Remote.IP, Name: "expired.example", Source: "dns", ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(-time.Second)},
		{IP: "203.0.113.99", Name: "other.example", Source: "tls_sni", ValidFrom: now.Add(-time.Second), ValidUntil: now.Add(time.Minute)},
	}, now)
	require.Equal(t, connection.Remote.IP, decision.DisplayName)
	require.Equal(t, model.ConfidenceIPOnly, decision.Confidence)
}

func TestClassifyDirection(t *testing.T) {
	local := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12")}
	tests := []struct {
		name string
		edit func(*model.ConnectionDelta)
		want model.Direction
	}{
		{name: "outbound", edit: func(c *model.ConnectionDelta) {}, want: model.DirectionOutbound},
		{name: "inbound", edit: func(c *model.ConnectionDelta) { c.Local.Port = 8080; c.Remote.Port = 52000 }, want: model.DirectionInbound},
		{name: "container", edit: func(c *model.ConnectionDelta) {
			c.Remote.IP = "172.18.0.3"
			c.Owner = model.OwnerRef{Kind: model.OwnerContainer, ContainerID: "one"}
		}, want: model.DirectionContainer},
		{name: "internal", edit: func(c *model.ConnectionDelta) { c.Remote.IP = "10.0.0.3" }, want: model.DirectionInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection := connectionFixture()
			test.edit(&connection)
			require.Equal(t, test.want, ClassifyDirection(connection, local))
		})
	}
}

func connectionFixture() model.ConnectionDelta {
	return model.ConnectionDelta{
		Protocol:      "tcp",
		Local:         model.Endpoint{IP: "10.0.0.2", Port: 42000, Confidence: model.ConfidenceIPOnly},
		Remote:        model.Endpoint{IP: "203.0.113.10", Port: 443, Confidence: model.ConfidenceIPOnly},
		Owner:         model.OwnerRef{Kind: model.OwnerProcess, PID: 42, Process: "curl"},
		BytesSent:     100,
		BytesReceived: 200,
	}
}
