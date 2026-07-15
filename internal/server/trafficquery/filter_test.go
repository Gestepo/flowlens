package trafficquery

import (
	"net/url"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestParseFilterAcceptsSharedFieldsAndDefaults(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	values := url.Values{
		"node": {"flowlens-node-1"}, "start": {"2026-07-14T09:00:00Z"}, "end": {"2026-07-14T10:00:00Z"},
		"direction": {"outbound"}, "owner": {"container:web"}, "domain": {"example.com"}, "confidence": {"confirmed"},
		"ip": {"203.0.113.10"}, "port": {"443"}, "protocol": {"tcp"}, "cursor": {encodeCursor(10)}, "limit": {"100"}, "sort": {"bytes"},
	}
	filter, err := ParseFilter(values, now, true)
	require.NoError(t, err)
	require.Equal(t, "flowlens-node-1", filter.NodeID)
	require.Equal(t, model.DirectionOutbound, filter.Direction)
	require.Equal(t, uint16(443), filter.Port)
	require.Equal(t, 100, filter.Limit)
	require.Equal(t, "bytes", filter.Sort)

	filter, err = ParseFilter(url.Values{"node": {"flowlens-node-1"}}, now, false)
	require.NoError(t, err)
	require.Equal(t, now.Add(-24*time.Hour), filter.Start)
	require.Equal(t, now, filter.End)
	require.Equal(t, 50, filter.Limit)
}

func TestParseFilterReturnsFieldSpecificErrors(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		field string
		value string
	}{
		{field: "node", value: ""},
		{field: "start", value: "not-time"},
		{field: "end", value: "not-time"},
		{field: "direction", value: "sideways"},
		{field: "confidence", value: "guessed"},
		{field: "ip", value: "bad-ip"},
		{field: "port", value: "70000"},
		{field: "limit", value: "201"},
		{field: "sort", value: "drop table"},
		{field: "cursor", value: "not-a-cursor"},
	}
	for _, test := range tests {
		t.Run(test.field, func(t *testing.T) {
			values := url.Values{"node": {"node"}, test.field: {test.value}}
			_, err := ParseFilter(values, now, true)
			require.ErrorContains(t, err, test.field)
		})
	}
	_, err := ParseFilter(url.Values{"node": {"node"}, "start": {now.Add(-31 * 24 * time.Hour).Format(time.RFC3339)}, "end": {now.Format(time.RFC3339)}}, now, true)
	require.ErrorContains(t, err, "30 days")
}
