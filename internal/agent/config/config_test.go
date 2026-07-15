package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadAppliesAgentDefaults(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_NODE_ID":         "flowlens-node-1",
		"FLOWLENS_SERVER_ENDPOINT": "http://127.0.0.1:8088/api/v1/agent/batches",
		"FLOWLENS_AGENT_TOKEN":     "agent-secret",
	}

	loaded, err := Load(func(key string) string { return values[key] })

	require.NoError(t, err)
	require.Equal(t, "flowlens-node-1", loaded.NodeID)
	require.Equal(t, "/var/lib/flowlens-agent/spool", loaded.SpoolDir)
	require.Equal(t, "/proc/net/dev", loaded.InterfaceCountersPath)
	require.Equal(t, 2*time.Second, loaded.Interval)
	require.True(t, loaded.DockerAttribution)
}

func TestLoadCanDisableDockerAttribution(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_NODE_ID": "node", "FLOWLENS_SERVER_ENDPOINT": "http://127.0.0.1:8088/api/v1/agent/batches", "FLOWLENS_AGENT_TOKEN": "secret",
		"FLOWLENS_DOCKER_ATTRIBUTION": "disabled",
	}
	loaded, err := Load(func(key string) string { return values[key] })
	require.NoError(t, err)
	require.False(t, loaded.DockerAttribution)

	values["FLOWLENS_DOCKER_ATTRIBUTION"] = "sometimes"
	_, err = Load(func(key string) string { return values[key] })
	require.ErrorContains(t, err, "FLOWLENS_DOCKER_ATTRIBUTION")
}

func TestLoadRejectsMissingAgentSettingsWithoutLeakingSecrets(t *testing.T) {
	values := map[string]string{"FLOWLENS_AGENT_TOKEN": "do-not-print-this"}

	_, err := Load(func(key string) string { return values[key] })

	require.Error(t, err)
	require.NotContains(t, err.Error(), "do-not-print-this")
	require.ErrorContains(t, err, "FLOWLENS_NODE_ID")
}

func TestLoadRejectsInvalidAgentInterval(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_NODE_ID":         "node",
		"FLOWLENS_SERVER_ENDPOINT": "http://127.0.0.1:8088/api/v1/agent/batches",
		"FLOWLENS_AGENT_TOKEN":     "secret",
		"FLOWLENS_INTERVAL":        "instant",
	}

	_, err := Load(func(key string) string { return values[key] })

	require.ErrorContains(t, err, "FLOWLENS_INTERVAL")
}

func TestLoadAcceptsOnlyNPMLogGlobsWithinAllowedRoots(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_NODE_ID":            "node",
		"FLOWLENS_SERVER_ENDPOINT":    "http://127.0.0.1:8088/api/v1/agent/batches",
		"FLOWLENS_AGENT_TOKEN":        "secret",
		"FLOWLENS_NPM_LOG_GLOBS":      "/data/logs/proxy-host-*_access.log, /var/lib/docker/volumes/npm/_data/logs/*.log",
		"FLOWLENS_CAPTURE_INTERFACES": "enp0s6,eth1",
	}
	loaded, err := Load(func(key string) string { return values[key] })
	require.NoError(t, err)
	require.Equal(t, []string{"/data/logs/proxy-host-*_access.log", "/var/lib/docker/volumes/npm/_data/logs/*.log"}, loaded.NPMLogGlobs)
	require.Equal(t, []string{"enp0s6", "eth1"}, loaded.CaptureInterfaces)

	values["FLOWLENS_NPM_LOG_GLOBS"] = "/etc/*.log"
	_, err = Load(func(key string) string { return values[key] })
	require.ErrorContains(t, err, "FLOWLENS_NPM_LOG_GLOBS")
}

func TestLoadRejectsUnsafeCaptureInterface(t *testing.T) {
	values := map[string]string{
		"FLOWLENS_NODE_ID": "node", "FLOWLENS_SERVER_ENDPOINT": "http://127.0.0.1:8088/api/v1/agent/batches", "FLOWLENS_AGENT_TOKEN": "secret",
		"FLOWLENS_CAPTURE_INTERFACES": "enp0s6;rm",
	}
	_, err := Load(func(key string) string { return values[key] })
	require.ErrorContains(t, err, "FLOWLENS_CAPTURE_INTERFACES")
}
