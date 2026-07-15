package deploy

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlowLensContainerDeploymentArtifactsAreRemoved(t *testing.T) {
	for _, path := range []string{"Dockerfile." + "server", "compose." + "yml", "compose.test." + "yml", "server." + "compose." + "env.example", "../.docker" + "ignore"} {
		require.NoFileExists(t, path)
	}
}

func TestAgentServiceGrantsOnlyDetailedCollectorAccess(t *testing.T) {
	service, err := os.ReadFile("flowlens-agent.service")
	require.NoError(t, err)
	contents := string(service)
	require.Contains(t, contents, "AmbientCapabilities=CAP_BPF CAP_PERFMON CAP_SYS_RESOURCE CAP_NET_RAW")
	require.Contains(t, contents, "CapabilityBoundingSet=CAP_BPF CAP_PERFMON CAP_SYS_RESOURCE CAP_NET_RAW")
	require.NotContains(t, contents, "SupplementaryGroups=docker")
	require.Contains(t, contents, "NoNewPrivileges=true")
	require.Contains(t, contents, "MemoryMax=96M")
	require.Contains(t, contents, "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK AF_PACKET")
}

func TestConfigureGeneratesOnlyNativeEnvironments(t *testing.T) {
	dir := t.TempDir()
	command := exec.Command("../scripts/configure-deploy.sh", dir)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))

	server := readEnvironment(t, filepath.Join(dir, "server.env"))
	agent := readEnvironment(t, filepath.Join(dir, "agent.env"))
	require.Contains(t, server["FLOWLENS_DATABASE_URL"], "@127.0.0.1:5432")
	require.Equal(t, "127.0.0.1:8088", server["FLOWLENS_LISTEN_ADDRESS"])
	require.Equal(t, server["FLOWLENS_AGENT_TOKEN"], agent["FLOWLENS_AGENT_TOKEN"])
	require.Len(t, agent["FLOWLENS_AGENT_TOKEN"], 64)
	require.Len(t, server["FLOWLENS_BOOTSTRAP_TOKEN"], 64)
	require.Len(t, server["FLOWLENS_SECRET_KEY"], 64)
	require.NotEqual(t, server["FLOWLENS_BOOTSTRAP_TOKEN"], server["FLOWLENS_SECRET_KEY"])
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, name := range []string{"server.env", "agent.env"} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
}

func TestConfigureMigratesLegacyServerEnvironmentWithoutRotatingSecrets(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.env")
	agentPath := filepath.Join(dir, "agent.env")
	serverContents := strings.Join([]string{
		"FLOWLENS_DATABASE_URL=postgres://flowlens:database-marker@postgresql:5432/flowlens?sslmode=disable",
		"FLOWLENS_AGENT_TOKEN=agent-marker",
		"FLOWLENS_BOOTSTRAP_TOKEN=bootstrap-marker",
		"FLOWLENS_SECRET_KEY=secret-marker",
		"FLOWLENS_LISTEN_ADDRESS=0.0.0.0:8088",
		"FLOWLENS_CUSTOM_MARKER=preserve-me",
		"",
	}, "\n")
	agentContents := "FLOWLENS_AGENT_TOKEN=agent-marker\nFLOWLENS_NODE_ID=existing-node\n"
	require.NoError(t, os.WriteFile(serverPath, []byte(serverContents), 0600))
	require.NoError(t, os.WriteFile(agentPath, []byte(agentContents), 0600))

	command := exec.Command("../scripts/configure-deploy.sh", dir)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	for _, marker := range []string{"database-marker", "agent-marker", "bootstrap-marker", "secret-marker"} {
		require.NotContains(t, string(output), marker)
	}
	nativeContents := strings.ReplaceAll(serverContents, "@postgresql:5432", "@127.0.0.1:5432")
	nativeContents = strings.ReplaceAll(nativeContents, "FLOWLENS_LISTEN_ADDRESS=0.0.0.0:8088", "FLOWLENS_LISTEN_ADDRESS=127.0.0.1:8088")
	require.Equal(t, nativeContents, string(mustReadFile(t, serverPath)))
	require.Equal(t, agentContents, string(mustReadFile(t, agentPath)))

	command = exec.Command("../scripts/configure-deploy.sh", dir)
	repeatOutput, err := command.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(repeatOutput), "refusing to overwrite")
}

func TestConfigureFinishesTrustedLegacyEnvironmentMigration(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.env")
	agentPath := filepath.Join(dir, "agent.env")
	legacyPath := filepath.Join(dir, "server."+"compose."+"env")
	legacyContents := "FLOWLENS_DATABASE_URL=postgres://flowlens:database-marker@postgresql:5432/flowlens\nFLOWLENS_LISTEN_ADDRESS=0.0.0.0:8088\nFLOWLENS_AGENT_TOKEN=agent-marker\n"
	nativeContents := strings.ReplaceAll(legacyContents, "@postgresql:5432", "@127.0.0.1:5432")
	nativeContents = strings.ReplaceAll(nativeContents, "FLOWLENS_LISTEN_ADDRESS=0.0.0.0:8088", "FLOWLENS_LISTEN_ADDRESS=127.0.0.1:8088")
	agentContents := "FLOWLENS_AGENT_TOKEN=agent-marker\n"
	require.NoError(t, os.WriteFile(serverPath, []byte(nativeContents), 0600))
	require.NoError(t, os.WriteFile(agentPath, []byte(agentContents), 0600))
	require.NoError(t, os.WriteFile(legacyPath, []byte(legacyContents), 0600))

	command := exec.Command("../scripts/configure-deploy.sh", dir)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.NoFileExists(t, legacyPath)
	require.Equal(t, nativeContents, string(mustReadFile(t, serverPath)))
	require.Equal(t, agentContents, string(mustReadFile(t, agentPath)))
}

func TestConfigureRejectsUntrustedLegacyEnvironment(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.env")
	agentPath := filepath.Join(dir, "agent.env")
	legacyPath := filepath.Join(dir, "server."+"compose."+"env")
	nativeContents := "FLOWLENS_DATABASE_URL=postgres://flowlens:database-marker@127.0.0.1:5432/flowlens\nFLOWLENS_LISTEN_ADDRESS=127.0.0.1:8088\nFLOWLENS_AGENT_TOKEN=agent-marker\n"
	require.NoError(t, os.WriteFile(serverPath, []byte(nativeContents), 0600))
	require.NoError(t, os.WriteFile(agentPath, []byte("FLOWLENS_AGENT_TOKEN=agent-marker\n"), 0600))
	require.NoError(t, os.WriteFile(legacyPath, []byte("FLOWLENS_DATABASE_URL=postgres://different\n"), 0600))

	command := exec.Command("../scripts/configure-deploy.sh", dir)
	output, err := command.CombinedOutput()
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "refusing")
	require.FileExists(t, legacyPath)
	require.Equal(t, nativeContents, string(mustReadFile(t, serverPath)))
}

func TestConfigureRejectsOrphanedLegacyEnvironment(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "server."+"compose."+"env")
	require.NoError(t, os.WriteFile(legacyPath, []byte("FLOWLENS_AGENT_TOKEN=orphaned\n"), 0600))

	command := exec.Command("../scripts/configure-deploy.sh", dir)
	output, err := command.CombinedOutput()
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "refusing")
	require.NoFileExists(t, filepath.Join(dir, "server.env"))
	require.NoFileExists(t, filepath.Join(dir, "agent.env"))
	require.FileExists(t, legacyPath)
}

func TestGeoIPUpdaterUsesMonthlyLiteFilesAndRollback(t *testing.T) {
	script, err := os.ReadFile("../scripts/update-geoip.sh")
	require.NoError(t, err)
	contents := string(script)
	require.Contains(t, contents, "dbip-country-lite-${period}.mmdb.gz")
	require.Contains(t, contents, "dbip-asn-lite-${period}.mmdb.gz")
	require.Contains(t, contents, "gzip -t")
	require.Contains(t, contents, "rollback")
	require.Contains(t, contents, "Authorization: Bearer")
	require.Contains(t, contents, "https://db-ip.com")
}

func readEnvironment(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[key] = value
		}
	}
	require.NoError(t, scanner.Err())
	return values
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return contents
}
