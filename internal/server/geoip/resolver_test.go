package geoip

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolverReturnsCountryAndASNForPublicIPv4AndIPv6(t *testing.T) {
	resolver := newResolver(
		&fakeDatabase{countries: map[string]countryRecord{
			"8.8.8.8":              {Country: countryDetail{ISOCode: "US", Names: map[string]string{"zh-CN": "美国", "en": "United States"}}},
			"2606:4700:4700::1111": {Country: countryDetail{ISOCode: "US", Names: map[string]string{"en": "United States"}}},
		}},
		&fakeDatabase{asns: map[string]asnRecord{
			"8.8.8.8":              {Number: 15169, Organization: "Google LLC"},
			"2606:4700:4700::1111": {Number: 13335, Organization: "Cloudflare, Inc."},
		}},
	)

	result := resolver.Lookup(netip.MustParseAddr("8.8.8.8"))
	require.Equal(t, "US", result.CountryCode)
	require.Equal(t, "美国", result.CountryName)
	require.Equal(t, uint32(15169), result.ASN)
	require.Equal(t, "Google LLC", result.Organization)
	require.Equal(t, "public", result.Classification)

	result = resolver.Lookup(netip.MustParseAddr("2606:4700:4700::1111"))
	require.Equal(t, uint32(13335), result.ASN)
}

func TestResolverClassifiesReservedAndMissingRecords(t *testing.T) {
	resolver := newResolver(&fakeDatabase{}, &fakeDatabase{})
	for _, value := range []string{"10.0.0.1", "127.0.0.1", "169.254.1.1", "192.0.2.1", "203.0.113.1", "2001:db8::1", "ff02::1"} {
		require.Equal(t, "local_or_reserved", resolver.Lookup(netip.MustParseAddr(value)).Classification, value)
	}
	require.Equal(t, "unknown", resolver.Lookup(netip.MustParseAddr("1.1.1.1")).Classification)
}

func TestFailedReloadPreservesExistingSnapshot(t *testing.T) {
	resolver := newResolver(
		&fakeDatabase{countries: map[string]countryRecord{"8.8.8.8": {Country: countryDetail{ISOCode: "US"}}}},
		&fakeDatabase{},
	)
	require.Error(t, resolver.Reload("/missing/country.mmdb", "/missing/asn.mmdb"))
	require.Equal(t, "US", resolver.Lookup(netip.MustParseAddr("8.8.8.8")).CountryCode)
	require.NoError(t, resolver.Close())
	require.NoError(t, resolver.Close())
}

type fakeDatabase struct {
	countries map[string]countryRecord
	asns      map[string]asnRecord
	closed    bool
}

func (database *fakeDatabase) Lookup(address netip.Addr, destination any) bool {
	switch value := destination.(type) {
	case *countryRecord:
		record, ok := database.countries[address.String()]
		*value = record
		return ok
	case *asnRecord:
		record, ok := database.asns[address.String()]
		*value = record
		return ok
	default:
		return false
	}
}

func (database *fakeDatabase) Close() error {
	database.closed = true
	return nil
}
