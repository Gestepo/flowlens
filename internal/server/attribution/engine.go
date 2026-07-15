package attribution

import (
	"net/netip"
	"sort"
	"time"

	"flowlens/internal/model"
)

type Decision struct {
	DisplayName        string
	Confidence         model.DomainConfidence
	EvidenceSource     string
	EvidenceObservedAt *time.Time
}

func DecideAt(connection model.ConnectionDelta, evidence []model.NameEvidence, observedAt time.Time) Decision {
	if connection.Remote.Domain != "" && connection.Remote.Confidence == model.ConfidenceConfirmed {
		return Decision{DisplayName: connection.Remote.Domain, Confidence: model.ConfidenceConfirmed, EvidenceSource: "connection"}
	}
	candidates := make([]model.NameEvidence, 0, len(evidence))
	for _, item := range evidence {
		if item.IP == connection.Remote.IP && !observedAt.Before(item.ValidFrom) && observedAt.Before(item.ValidUntil) {
			candidates = append(candidates, item)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := evidenceStrength(candidates[i].Source), evidenceStrength(candidates[j].Source)
		if left != right {
			return left > right
		}
		if !candidates[i].ValidFrom.Equal(candidates[j].ValidFrom) {
			return candidates[i].ValidFrom.After(candidates[j].ValidFrom)
		}
		return candidates[i].Name < candidates[j].Name
	})
	if len(candidates) == 0 {
		return Decision{DisplayName: connection.Remote.IP, Confidence: model.ConfidenceIPOnly}
	}
	chosen := candidates[0]
	confidence := model.ConfidenceInferred
	if chosen.Source == "tls_sni" {
		confidence = model.ConfidenceConfirmed
	}
	observed := chosen.ValidFrom
	return Decision{DisplayName: chosen.Name, Confidence: confidence, EvidenceSource: chosen.Source, EvidenceObservedAt: &observed}
}

func ClassifyDirection(connection model.ConnectionDelta, localPrefixes []netip.Prefix) model.Direction {
	localAddress, localErr := netip.ParseAddr(connection.Local.IP)
	remoteAddress, remoteErr := netip.ParseAddr(connection.Remote.IP)
	if localErr != nil || remoteErr != nil {
		return model.DirectionInternal
	}
	localIsLocal := isLocal(localAddress, localPrefixes)
	remoteIsLocal := isLocal(remoteAddress, localPrefixes)
	if localIsLocal && remoteIsLocal {
		if connection.Owner.Kind == model.OwnerContainer {
			return model.DirectionContainer
		}
		return model.DirectionInternal
	}
	if localIsLocal && !remoteIsLocal {
		if connection.Local.Port <= 32767 && connection.Remote.Port > 32767 {
			return model.DirectionInbound
		}
		return model.DirectionOutbound
	}
	if !localIsLocal && remoteIsLocal {
		return model.DirectionInbound
	}
	return model.DirectionOutbound
}

func evidenceStrength(source string) int {
	switch source {
	case "tls_sni":
		return 2
	case "dns":
		return 1
	default:
		return 0
	}
}

func isLocal(address netip.Addr, prefixes []netip.Prefix) bool {
	if address.IsLoopback() || address.IsLinkLocalUnicast() {
		return true
	}
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
