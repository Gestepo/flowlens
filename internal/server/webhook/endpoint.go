package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func ValidateEndpoint(ctx context.Context, raw string, resolver Resolver, allowHTTP bool) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, errors.New("webhook endpoint is invalid")
	}
	if endpoint.Scheme != "https" && !(allowHTTP && endpoint.Scheme == "http") {
		return nil, errors.New("webhook endpoint must use HTTPS")
	}
	if err := requirePublicHost(ctx, endpoint.Hostname(), resolver); err != nil {
		return nil, err
	}
	return endpoint, nil
}

type SafeDialer struct {
	Resolver Resolver
	Dialer   net.Dialer
}

func (dialer SafeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse webhook address: %w", err)
	}
	addresses, err := resolveHost(ctx, host, dialer.Resolver)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return nil, errors.New("webhook host must resolve only to public addresses")
		}
	}
	base := dialer.Dialer
	if base.Timeout == 0 {
		base.Timeout = 10 * time.Second
	}
	return base.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
}

func requirePublicHost(ctx context.Context, host string, resolver Resolver) error {
	addresses, err := resolveHost(ctx, host, resolver)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return errors.New("webhook host must resolve only to public addresses")
		}
	}
	return nil
}

func resolveHost(ctx context.Context, host string, resolver Resolver) ([]net.IPAddr, error) {
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return []net.IPAddr{{IP: ip}}, nil
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("webhook host could not be resolved")
	}
	return addresses, nil
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}
