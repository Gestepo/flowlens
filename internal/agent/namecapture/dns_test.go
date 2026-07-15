package namecapture

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/require"
)

func TestParseDNSMessageEmitsSuccessfulAddressAnswersWithClampedTTL(t *testing.T) {
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	payload := serializeDNS(t, layers.DNS{
		ID: 7, QR: true, ResponseCode: layers.DNSResponseCodeNoErr,
		Answers: []layers.DNSResourceRecord{
			{Name: []byte("Example.COM"), Type: layers.DNSTypeA, Class: layers.DNSClassIN, TTL: 1, IP: net.IPv4(203, 0, 113, 10)},
			{Name: []byte("V6.Example.COM"), Type: layers.DNSTypeAAAA, Class: layers.DNSClassIN, TTL: 90000, IP: net.ParseIP("2001:db8::10")},
			{Name: []byte("alias.example.com"), Type: layers.DNSTypeCNAME, Class: layers.DNSClassIN, TTL: 60, CNAME: []byte("example.com")},
		},
	})

	evidence, err := ParseDNSMessage(payload, now)
	require.NoError(t, err)
	require.Len(t, evidence, 2)
	require.Equal(t, "example.com", evidence[0].Name)
	require.Equal(t, "203.0.113.10", evidence[0].IP)
	require.Equal(t, now.Add(30*time.Second), evidence[0].ValidUntil)
	require.Equal(t, "v6.example.com", evidence[1].Name)
	require.Equal(t, "2001:db8::10", evidence[1].IP)
	require.Equal(t, now.Add(24*time.Hour), evidence[1].ValidUntil)
	event := EvidenceEvent(evidence[0])
	require.Equal(t, "name_evidence", string(event.Kind))
	require.Equal(t, evidence[0], *event.NameEvidence)
}

func TestParseDNSMessageSupportsTCPAndRejectsNXDOMAIN(t *testing.T) {
	now := time.Now().UTC()
	nxdomain := serializeDNS(t, layers.DNS{ID: 9, QR: true, ResponseCode: layers.DNSResponseCodeNXDomain})
	evidence, err := ParseDNSMessage(nxdomain, now)
	require.NoError(t, err)
	require.Empty(t, evidence)

	udp := serializeDNS(t, layers.DNS{ID: 10, QR: true, ResponseCode: layers.DNSResponseCodeNoErr, Answers: []layers.DNSResourceRecord{
		{Name: []byte("tcp.example.test"), Type: layers.DNSTypeA, Class: layers.DNSClassIN, TTL: 60, IP: net.IPv4(192, 0, 2, 20)},
	}})
	tcp := make([]byte, len(udp)+2)
	binary.BigEndian.PutUint16(tcp[:2], uint16(len(udp)))
	copy(tcp[2:], udp)
	evidence, err = ParseDNSMessage(tcp, now)
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	require.Equal(t, "tcp.example.test", evidence[0].Name)
}

func serializeDNS(t *testing.T, dns layers.DNS) []byte {
	t.Helper()
	buffer := gopacket.NewSerializeBuffer()
	require.NoError(t, dns.SerializeTo(buffer, gopacket.SerializeOptions{FixLengths: true}))
	return append([]byte(nil), buffer.Bytes()...)
}
