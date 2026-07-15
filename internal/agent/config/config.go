package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type LookupFunc func(string) string

type Config struct {
	NodeID                string
	ServerEndpoint        string
	AgentToken            string
	SpoolDir              string
	InterfaceCountersPath string
	NPMLogGlobs           []string
	CaptureInterfaces     []string
	DockerAttribution     bool
	Interval              time.Duration
}

func FromEnv() (Config, error) {
	return Load(os.Getenv)
}

func Load(lookup LookupFunc) (Config, error) {
	config := Config{
		NodeID:                lookup("FLOWLENS_NODE_ID"),
		ServerEndpoint:        lookup("FLOWLENS_SERVER_ENDPOINT"),
		AgentToken:            lookup("FLOWLENS_AGENT_TOKEN"),
		SpoolDir:              valueOrDefault(lookup("FLOWLENS_SPOOL_DIR"), "/var/lib/flowlens-agent/spool"),
		InterfaceCountersPath: valueOrDefault(lookup("FLOWLENS_INTERFACE_COUNTERS_PATH"), "/proc/net/dev"),
		DockerAttribution:     true,
		Interval:              2 * time.Second,
	}
	if config.NodeID == "" {
		return Config{}, errors.New("FLOWLENS_NODE_ID is required")
	}
	if config.ServerEndpoint == "" {
		return Config{}, errors.New("FLOWLENS_SERVER_ENDPOINT is required")
	}
	if config.AgentToken == "" {
		return Config{}, errors.New("FLOWLENS_AGENT_TOKEN is required")
	}
	if value := lookup("FLOWLENS_INTERVAL"); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil || interval <= 0 {
			return Config{}, fmt.Errorf("FLOWLENS_INTERVAL must be a positive duration")
		}
		config.Interval = interval
	}
	if value := strings.TrimSpace(lookup("FLOWLENS_DOCKER_ATTRIBUTION")); value != "" {
		switch value {
		case "enabled":
			config.DockerAttribution = true
		case "disabled":
			config.DockerAttribution = false
		default:
			return Config{}, fmt.Errorf("FLOWLENS_DOCKER_ATTRIBUTION must be enabled or disabled")
		}
	}
	if value := lookup("FLOWLENS_NPM_LOG_GLOBS"); value != "" {
		for _, pattern := range strings.Split(value, ",") {
			pattern = filepath.Clean(strings.TrimSpace(pattern))
			if pattern == "." || !withinAllowedNPMRoot(pattern) {
				return Config{}, fmt.Errorf("FLOWLENS_NPM_LOG_GLOBS contains a path outside allowed roots")
			}
			config.NPMLogGlobs = append(config.NPMLogGlobs, pattern)
		}
	}
	if value := lookup("FLOWLENS_CAPTURE_INTERFACES"); value != "" {
		validInterface := regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)
		for _, name := range strings.Split(value, ",") {
			name = strings.TrimSpace(name)
			if !validInterface.MatchString(name) {
				return Config{}, fmt.Errorf("FLOWLENS_CAPTURE_INTERFACES contains an invalid interface")
			}
			config.CaptureInterfaces = append(config.CaptureInterfaces, name)
		}
	}
	return config, nil
}

func withinAllowedNPMRoot(path string) bool {
	for _, root := range []string{"/data/logs", "/var/lib/docker/volumes"} {
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
