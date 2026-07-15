package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type LookupFunc func(string) string

type Config struct {
	DatabaseURL         string
	AgentToken          string
	BootstrapToken      string
	SecretKey           []byte
	PublicURL           string
	WebhookAllowHTTP    bool
	ListenAddress       string
	WebDir              string
	GeoIPCountryPath    string
	GeoIPASNPath        string
	DatabaseBudgetBytes int64
	ShutdownTimeout     time.Duration
}

func FromEnv() (Config, error) {
	return Load(os.Getenv)
}

func Load(lookup LookupFunc) (Config, error) {
	config := Config{
		DatabaseURL:         lookup("FLOWLENS_DATABASE_URL"),
		AgentToken:          lookup("FLOWLENS_AGENT_TOKEN"),
		BootstrapToken:      lookup("FLOWLENS_BOOTSTRAP_TOKEN"),
		PublicURL:           lookup("FLOWLENS_PUBLIC_URL"),
		ListenAddress:       valueOrDefault(lookup("FLOWLENS_LISTEN_ADDRESS"), "127.0.0.1:8088"),
		WebDir:              valueOrDefault(lookup("FLOWLENS_WEB_DIR"), "/opt/flowlens/web"),
		GeoIPCountryPath:    valueOrDefault(lookup("FLOWLENS_GEOIP_COUNTRY_PATH"), "/var/lib/flowlens/geoip/country.mmdb"),
		GeoIPASNPath:        valueOrDefault(lookup("FLOWLENS_GEOIP_ASN_PATH"), "/var/lib/flowlens/geoip/asn.mmdb"),
		DatabaseBudgetBytes: 10 << 30,
		ShutdownTimeout:     10 * time.Second,
	}
	if config.DatabaseURL == "" {
		return Config{}, errors.New("FLOWLENS_DATABASE_URL is required")
	}
	if config.AgentToken == "" {
		return Config{}, errors.New("FLOWLENS_AGENT_TOKEN is required")
	}
	if config.BootstrapToken == "" {
		return Config{}, errors.New("FLOWLENS_BOOTSTRAP_TOKEN is required")
	}
	secretKey, err := hex.DecodeString(lookup("FLOWLENS_SECRET_KEY"))
	if err != nil || len(secretKey) != 32 {
		return Config{}, errors.New("FLOWLENS_SECRET_KEY must be 64 hexadecimal characters")
	}
	config.SecretKey = secretKey
	publicURL, err := url.Parse(config.PublicURL)
	if err != nil || publicURL.Scheme != "https" || publicURL.Hostname() == "" || publicURL.User != nil {
		return Config{}, errors.New("FLOWLENS_PUBLIC_URL must be an HTTPS URL")
	}
	if value := lookup("FLOWLENS_WEBHOOK_ALLOW_HTTP"); value != "" {
		allow, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, errors.New("FLOWLENS_WEBHOOK_ALLOW_HTTP must be true or false")
		}
		config.WebhookAllowHTTP = allow
	}
	if value := lookup("FLOWLENS_SHUTDOWN_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("FLOWLENS_SHUTDOWN_TIMEOUT must be a positive duration")
		}
		config.ShutdownTimeout = timeout
	}
	if value := lookup("FLOWLENS_DATABASE_BUDGET_BYTES"); value != "" {
		budget, err := strconv.ParseInt(value, 10, 64)
		if err != nil || budget <= 0 {
			return Config{}, errors.New("FLOWLENS_DATABASE_BUDGET_BYTES must be a positive integer")
		}
		config.DatabaseBudgetBytes = budget
	}
	return config, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
