package npm

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"flowlens/internal/model"

	"github.com/stretchr/testify/require"
)

func TestParseLineExtractsOnlyRequestMetadata(t *testing.T) {
	file, err := os.Open("testdata/access.log")
	require.NoError(t, err)
	defer file.Close()
	var parsed []ParsedRequest
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		request, err := ParseLine(scanner.Text())
		require.NoError(t, err)
		parsed = append(parsed, request)
	}
	require.NoError(t, scanner.Err())
	require.Len(t, parsed, 4)

	require.Equal(t, "app.example.test", parsed[0].Request.Host)
	require.Equal(t, "198.51.100.4", parsed[0].Request.SourceIP)
	require.Equal(t, "GET", parsed[0].Request.Method)
	require.Equal(t, 200, parsed[0].Request.Status)
	require.Equal(t, int64(8437), parsed[0].Request.BytesSent)
	require.Equal(t, "web", parsed[0].Request.Upstream)
	require.Zero(t, parsed[0].Request.DurationMS)
	require.Equal(t, 502, parsed[1].Request.Status)
	require.Equal(t, "2001:db8::4", parsed[2].Request.SourceIP)
	require.Equal(t, "OPTIONS", parsed[2].Request.Method)

	encoded, err := json.Marshal(parsed)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-value")
	require.NotContains(t, string(encoded), "discard-me")
	require.NotContains(t, string(encoded), "quoted")
}

func TestParseLineRejectsMalformedInput(t *testing.T) {
	_, err := ParseLine("not an NPM access log line")
	require.ErrorContains(t, err, "NPM access log")
}

func TestParseLineAcceptsRedirectStatusInSecondStatusField(t *testing.T) {
	parsed, err := ParseLine(`[15/Jul/2026:06:00:47 +0000] - - 301 - GET http monitor.example.com "/" [Client 192.0.2.10] [Length 166] [Gzip -] [Sent-to 172.17.0.1] "agent" "-"`)
	require.NoError(t, err)
	require.Equal(t, 301, parsed.Request.Status)
	require.Equal(t, "monitor.example.com", parsed.Request.Host)
	require.Equal(t, int64(166), parsed.Request.BytesSent)
}

func TestParseLinePrefersFirstNumericStatusField(t *testing.T) {
	parsed, err := ParseLine(`[15/Jul/2026:06:00:47 +0000] - 200 301 - GET http monitor.example.com "/" [Client 192.0.2.10] [Length 166] [Gzip -] [Sent-to 172.17.0.1] "agent" "-"`)
	require.NoError(t, err)
	require.Equal(t, 200, parsed.Request.Status)
}

func TestParseLineRejectsMissingStatusInBothFields(t *testing.T) {
	_, err := ParseLine(`[15/Jul/2026:06:00:47 +0000] - - - - GET http monitor.example.com "/" [Client 192.0.2.10] [Length 166] [Gzip -] [Sent-to 172.17.0.1] "agent" "-"`)
	require.ErrorContains(t, err, "status")
}

func TestEventsIncludeRequestsAndCollectorHealth(t *testing.T) {
	parsed, err := ParseLine(fixtureLines(t)[0])
	require.NoError(t, err)
	at := time.Date(2026, 7, 14, 16, 0, 0, 0, time.UTC)
	events := Events(PollResult{Requests: []ParsedRequest{parsed}, Malformed: 2, Truncated: true}, at)
	require.Len(t, events, 2)
	require.Equal(t, model.EventProxyRequest, events[0].Kind)
	require.Equal(t, parsed.ObservedAt, events[0].ObservedAt)
	require.Equal(t, model.EventHealth, events[1].Kind)
	require.Equal(t, "npm_logs", events[1].Health.Collector)
	require.Equal(t, int64(2), events[1].Health.DroppedEvents)
	require.Equal(t, "log_truncated", events[1].Health.Code)
	unavailable := CollectorUnavailableEvent(at)
	require.Equal(t, "collector_unavailable", unavailable.Health.Code)
	require.NotContains(t, unavailable.Health.Message, "/")
}

func TestEventsKeepIdenticalRequestsFromDifferentLogOffsets(t *testing.T) {
	parsed, err := ParseLine(fixtureLines(t)[0])
	require.NoError(t, err)
	first := parsed
	first.SourceID = "1:2:100"
	second := parsed
	second.SourceID = "1:2:200"

	events := Events(PollResult{Requests: []ParsedRequest{first, second}}, time.Now().UTC())
	require.Len(t, events, 2)
	require.NotEqual(t, events[0].ID, events[1].ID)
}

func TestRecoveryTrackerReportsInitialHealthAndRecoveryOnly(t *testing.T) {
	at := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	tracker := RecoveryTracker{}

	initial := tracker.Observe(PollResult{}, nil, at)
	require.NotNil(t, initial)
	require.Equal(t, "healthy", initial.Health.Status)
	require.Equal(t, "active", initial.Health.Code)
	require.Nil(t, tracker.Observe(PollResult{}, nil, at.Add(time.Second)))

	require.Nil(t, tracker.Observe(PollResult{Malformed: 1}, nil, at.Add(2*time.Second)))
	recovered := tracker.Observe(PollResult{}, nil, at.Add(3*time.Second))
	require.NotNil(t, recovered)
	require.Equal(t, "healthy", recovered.Health.Status)
	require.Nil(t, tracker.Observe(PollResult{}, nil, at.Add(4*time.Second)))

	require.Nil(t, tracker.Observe(PollResult{}, errors.New("unreadable"), at.Add(5*time.Second)))
	require.NotNil(t, tracker.Observe(PollResult{}, nil, at.Add(6*time.Second)))
}
