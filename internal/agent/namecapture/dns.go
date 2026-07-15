package namecapture

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"flowlens/internal/model"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func ParseDNSMessage(payload []byte, observedAt time.Time) ([]model.NameEvidence, error) {
	if len(payload) >= 2 && int(binary.BigEndian.Uint16(payload[:2])) == len(payload)-2 {
		payload = payload[2:]
	}
	var dns layers.DNS
	if err := dns.DecodeFromBytes(payload, gopacket.NilDecodeFeedback); err != nil {
		return nil, fmt.Errorf("decode DNS message: %w", err)
	}
	if !dns.QR || dns.ResponseCode != layers.DNSResponseCodeNoErr {
		return nil, nil
	}
	evidence := make([]model.NameEvidence, 0, len(dns.Answers))
	for _, answer := range dns.Answers {
		if answer.Class != layers.DNSClassIN || (answer.Type != layers.DNSTypeA && answer.Type != layers.DNSTypeAAAA) || answer.IP == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(string(answer.Name), "."))
		if name == "" {
			continue
		}
		ttl := time.Duration(answer.TTL) * time.Second
		if ttl < 30*time.Second {
			ttl = 30 * time.Second
		}
		if ttl > 24*time.Hour {
			ttl = 24 * time.Hour
		}
		evidence = append(evidence, model.NameEvidence{
			IP:         answer.IP.String(),
			Name:       name,
			Source:     "dns",
			ValidFrom:  observedAt.UTC(),
			ValidUntil: observedAt.UTC().Add(ttl),
		})
	}
	return evidence, nil
}
