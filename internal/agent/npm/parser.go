package npm

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"flowlens/internal/model"
)

var accessLine = regexp.MustCompile(`^\[([^]]+)\] - ([0-9]{3}|-) ([0-9]{3}|-) - ([A-Z]+) [^ ]+ ([^ ]+) "(?:\\.|[^"])*" \[Client ([^]]+)\] \[Length ([0-9]+)\] \[Gzip [^]]*\] \[Sent-to ([^]]+)\]`)

type ParsedRequest struct {
	ObservedAt time.Time          `json:"observed_at"`
	Request    model.ProxyRequest `json:"request"`
	SourceID   string             `json:"-"`
}

func ParseLine(line string) (ParsedRequest, error) {
	match := accessLine.FindStringSubmatch(line)
	if len(match) != 9 {
		return ParsedRequest{}, fmt.Errorf("invalid NPM access log line")
	}
	observedAt, err := time.Parse("02/Jan/2006:15:04:05 -0700", match[1])
	if err != nil {
		return ParsedRequest{}, fmt.Errorf("invalid NPM access log time: %w", err)
	}
	status := 0
	statusFound := false
	for _, candidate := range match[2:4] {
		if candidate == "-" {
			continue
		}
		status, err = strconv.Atoi(candidate)
		if err == nil {
			statusFound = true
			break
		}
	}
	if !statusFound {
		return ParsedRequest{}, fmt.Errorf("invalid NPM access log status")
	}
	if _, err := netip.ParseAddr(match[6]); err != nil {
		return ParsedRequest{}, fmt.Errorf("invalid NPM access log client IP")
	}
	bytesSent, err := strconv.ParseInt(match[7], 10, 64)
	if err != nil || bytesSent < 0 {
		return ParsedRequest{}, fmt.Errorf("invalid NPM access log length")
	}
	return ParsedRequest{
		ObservedAt: observedAt.UTC(),
		Request: model.ProxyRequest{
			Host:      strings.ToLower(strings.TrimSuffix(match[5], ".")),
			SourceIP:  match[6],
			Method:    match[4],
			Status:    status,
			BytesSent: bytesSent,
			Upstream:  match[8],
		},
	}, nil
}
