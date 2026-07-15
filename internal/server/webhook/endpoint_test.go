package webhook

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateEndpointRejectsUnsafeSchemesAndNetworks(t *testing.T) {
	tests := []struct{ name, endpoint, ip string }{
		{"http", "http://hooks.example.test/flowlens", "203.0.113.10"},
		{"loopback", "https://hooks.example.test/flowlens", "127.0.0.1"},
		{"private", "https://hooks.example.test/flowlens", "10.0.0.8"},
		{"link local", "https://hooks.example.test/flowlens", "169.254.169.254"},
		{"multicast", "https://hooks.example.test/flowlens", "224.0.0.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateEndpoint(context.Background(), test.endpoint, staticResolver(test.ip), false)
			require.Error(t, err)
		})
	}
	endpoint, err := ValidateEndpoint(context.Background(), "https://hooks.example.test/flowlens", staticResolver("203.0.113.10"), false)
	require.NoError(t, err)
	require.Equal(t, "hooks.example.test", endpoint.Hostname())
}

func TestValidateEndpointAllowsHTTPOnlyInExplicitDevelopmentMode(t *testing.T) {
	_, err := ValidateEndpoint(context.Background(), "http://hooks.example.test/flowlens", staticResolver("203.0.113.10"), true)
	require.NoError(t, err)
}

func TestSafeDialerRejectsDNSRebindingBeforeConnecting(t *testing.T) {
	resolver := &sequenceResolver{answers: [][]net.IPAddr{{{IP: net.ParseIP("203.0.113.10")}}, {{IP: net.ParseIP("127.0.0.1")}}}}
	_, err := ValidateEndpoint(context.Background(), "https://hooks.example.test/flowlens", resolver, false)
	require.NoError(t, err)
	dialer := SafeDialer{Resolver: resolver}
	connection, err := dialer.DialContext(context.Background(), "tcp", "hooks.example.test:443")
	if connection != nil {
		connection.Close()
	}
	require.ErrorContains(t, err, "public")
}

type staticResolver string

func (resolver staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP(string(resolver))}}, nil
}

type sequenceResolver struct {
	answers [][]net.IPAddr
	index   int
}

func (resolver *sequenceResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	answer := resolver.answers[resolver.index]
	if resolver.index < len(resolver.answers)-1 {
		resolver.index++
	}
	return answer, nil
}
