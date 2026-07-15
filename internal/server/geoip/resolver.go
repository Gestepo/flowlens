package geoip

import (
	"net/netip"
	"sync"

	maxminddb "github.com/oschwald/maxminddb-golang/v2"
)

type NetworkInfo struct {
	CountryCode    string `json:"country_code"`
	CountryName    string `json:"country_name"`
	ASN            uint32 `json:"asn"`
	Organization   string `json:"organization"`
	Classification string `json:"classification"`
}

type database interface {
	Lookup(netip.Addr, any) bool
	Close() error
}

type maxmindDatabase struct{ reader *maxminddb.Reader }

func (database *maxmindDatabase) Lookup(address netip.Addr, destination any) bool {
	result := database.reader.Lookup(address)
	if !result.Found() || result.Err() != nil {
		return false
	}
	return result.Decode(destination) == nil
}

func (database *maxmindDatabase) Close() error { return database.reader.Close() }

type countryRecord struct {
	Country countryDetail `maxminddb:"country"`
}

type countryDetail struct {
	ISOCode string            `maxminddb:"iso_code"`
	Names   map[string]string `maxminddb:"names"`
}

type asnRecord struct {
	Number       uint32 `maxminddb:"autonomous_system_number"`
	Organization string `maxminddb:"autonomous_system_organization"`
}

type Resolver struct {
	mu        sync.RWMutex
	country   database
	asn       database
	closeOnce sync.Once
	closeErr  error
}

func New() *Resolver { return &Resolver{} }

func newResolver(country, asn database) *Resolver { return &Resolver{country: country, asn: asn} }

func (resolver *Resolver) Lookup(address netip.Addr) NetworkInfo {
	if isReserved(address) {
		return NetworkInfo{Classification: "local_or_reserved"}
	}
	resolver.mu.RLock()
	defer resolver.mu.RUnlock()
	if resolver.country == nil && resolver.asn == nil {
		return NetworkInfo{Classification: "unknown"}
	}
	result := NetworkInfo{Classification: "unknown"}
	var country countryRecord
	if resolver.country != nil && resolver.country.Lookup(address, &country) {
		result.CountryCode = country.Country.ISOCode
		result.CountryName = country.Country.Names["zh-CN"]
		if result.CountryName == "" {
			result.CountryName = country.Country.Names["en"]
		}
		result.Classification = "public"
	}
	var asn asnRecord
	if resolver.asn != nil && resolver.asn.Lookup(address, &asn) {
		result.ASN = asn.Number
		result.Organization = asn.Organization
		result.Classification = "public"
	}
	return result
}

func (resolver *Resolver) Reload(countryPath, asnPath string) error {
	countryReader, err := maxminddb.Open(countryPath)
	if err != nil {
		return err
	}
	asnReader, err := maxminddb.Open(asnPath)
	if err != nil {
		_ = countryReader.Close()
		return err
	}
	newCountry := &maxmindDatabase{reader: countryReader}
	newASN := &maxmindDatabase{reader: asnReader}
	resolver.mu.Lock()
	oldCountry, oldASN := resolver.country, resolver.asn
	resolver.country, resolver.asn = newCountry, newASN
	if oldCountry != nil {
		_ = oldCountry.Close()
	}
	if oldASN != nil {
		_ = oldASN.Close()
	}
	resolver.mu.Unlock()
	return nil
}

func (resolver *Resolver) Close() error {
	resolver.closeOnce.Do(func() {
		resolver.mu.Lock()
		defer resolver.mu.Unlock()
		if resolver.country != nil {
			resolver.closeErr = resolver.country.Close()
			resolver.country = nil
		}
		if resolver.asn != nil {
			if err := resolver.asn.Close(); resolver.closeErr == nil {
				resolver.closeErr = err
			}
			resolver.asn = nil
		}
	})
	return resolver.closeErr
}

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func isReserved(address netip.Addr) bool {
	if !address.IsValid() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
