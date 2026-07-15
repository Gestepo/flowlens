package geoip

import (
	"net/netip"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDBIPLiteFilesDecodeCountryAndASN(t *testing.T) {
	countryPath := os.Getenv("FLOWLENS_TEST_COUNTRY_MMDB")
	asnPath := os.Getenv("FLOWLENS_TEST_ASN_MMDB")
	if countryPath == "" || asnPath == "" {
		t.Skip("FLOWLENS_TEST_COUNTRY_MMDB and FLOWLENS_TEST_ASN_MMDB are not configured")
	}
	resolver := New()
	t.Cleanup(func() { require.NoError(t, resolver.Close()) })
	require.NoError(t, resolver.Reload(countryPath, asnPath))
	result := resolver.Lookup(netip.MustParseAddr("8.8.8.8"))
	require.NotEmpty(t, result.CountryCode)
	require.NotZero(t, result.ASN)
	require.Equal(t, "public", result.Classification)
}
