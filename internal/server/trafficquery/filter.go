package trafficquery

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"flowlens/internal/model"
)

type Filter struct {
	NodeID     string
	Start      time.Time
	End        time.Time
	Direction  model.Direction
	OwnerID    string
	Domain     string
	Confidence model.DomainConfidence
	IP         netip.Addr
	Port       uint16
	Protocol   string
	Cursor     string
	Limit      int
	Sort       string
}

func ParseFilter(values url.Values, now time.Time, detail bool) (Filter, error) {
	filter := Filter{
		NodeID: values.Get("node"), OwnerID: values.Get("owner"), Domain: strings.ToLower(values.Get("domain")),
		Cursor: values.Get("cursor"), Limit: 50, Sort: "bytes", End: now.UTC(), Start: now.UTC().Add(-24 * time.Hour),
	}
	if filter.NodeID == "" || len(filter.NodeID) > 128 {
		return Filter{}, fmt.Errorf("node must contain 1..128 bytes")
	}
	if raw := values.Get("start"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return Filter{}, fmt.Errorf("start must be an RFC3339 UTC timestamp")
		}
		filter.Start = parsed.UTC()
	}
	if raw := values.Get("end"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return Filter{}, fmt.Errorf("end must be an RFC3339 UTC timestamp")
		}
		filter.End = parsed.UTC()
	}
	if !filter.Start.Before(filter.End) {
		return Filter{}, fmt.Errorf("start must be before end")
	}
	if detail && filter.End.Sub(filter.Start) > 30*24*time.Hour {
		return Filter{}, fmt.Errorf("detail range must not exceed 30 days")
	}
	if raw := values.Get("direction"); raw != "" {
		filter.Direction = model.Direction(raw)
		if !validDirection(filter.Direction) {
			return Filter{}, fmt.Errorf("direction is invalid")
		}
	}
	if raw := values.Get("confidence"); raw != "" {
		filter.Confidence = model.DomainConfidence(raw)
		if !validConfidence(filter.Confidence) {
			return Filter{}, fmt.Errorf("confidence is invalid")
		}
	}
	if raw := values.Get("ip"); raw != "" {
		address, err := netip.ParseAddr(raw)
		if err != nil {
			return Filter{}, fmt.Errorf("ip is invalid")
		}
		filter.IP = address
	}
	if raw := values.Get("port"); raw != "" {
		port, err := strconv.ParseUint(raw, 10, 16)
		if err != nil || port == 0 {
			return Filter{}, fmt.Errorf("port must be within 1..65535")
		}
		filter.Port = uint16(port)
	}
	if raw := values.Get("protocol"); raw != "" {
		if raw != "tcp" && raw != "udp" {
			return Filter{}, fmt.Errorf("protocol must be tcp or udp")
		}
		filter.Protocol = raw
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			return Filter{}, fmt.Errorf("limit must be within 1..200")
		}
		filter.Limit = limit
	}
	if raw := values.Get("sort"); raw != "" {
		filter.Sort = raw
	}
	if _, ok := map[string]struct{}{"bytes": {}, "connections": {}, "requests": {}, "time": {}}[filter.Sort]; !ok {
		return Filter{}, fmt.Errorf("sort is invalid")
	}
	if len(filter.Cursor) > 512 {
		return Filter{}, fmt.Errorf("cursor is too long")
	}
	if filter.Cursor != "" && encodeCursor(decodeCursor(filter.Cursor)) != filter.Cursor {
		return Filter{}, fmt.Errorf("cursor is invalid")
	}
	return filter, nil
}

func validDirection(direction model.Direction) bool {
	switch direction {
	case model.DirectionInbound, model.DirectionOutbound, model.DirectionInternal, model.DirectionContainer:
		return true
	default:
		return false
	}
}

func validConfidence(confidence model.DomainConfidence) bool {
	switch confidence {
	case model.ConfidenceConfirmed, model.ConfidenceInferred, model.ConfidenceIPOnly:
		return true
	default:
		return false
	}
}
