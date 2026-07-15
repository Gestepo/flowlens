package scripts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAttributionVerifierUsesBrowserSessionCookie(t *testing.T) {
	contents := readScript(t, "verify-attribution.sh")
	for _, fragment := range []string{"FLOWLENS_COOKIE_FILE", "--cookie", "inbound_flow_ok", "baseline_inbound_requests", "baseline_flow_requests", "systemctl show flowlens-server.service -p MainPID", "process:$server_pid:$server_process", "remote_port=8088", "direction=inbound", "domain=$public_domain", "FLOWLENS_DOCKER_ATTRIBUTION", "docker_attribution", "kind=process"} {
		if !strings.Contains(contents, fragment) {
			t.Fatalf("verify-attribution.sh must contain %q", fragment)
		}
	}
	if !strings.Contains(contents, "client=${container:-disabled}") {
		t.Fatal("verify-attribution.sh must report the controlled traffic identifier")
	}
}

func TestOperationalScriptsDoNotRequireFlowLensContainers(t *testing.T) {
	for _, name := range []string{"acceptance.sh", "backup.sh", "configure-deploy.sh", "restore.sh", "uninstall.sh", "verify-attribution.sh"} {
		contents := readScript(t, name)
		for _, forbidden := range []*regexp.Regexp{
			regexp.MustCompile(`docker[[:space:]]+compose`),
			regexp.MustCompile(`flowlens-(server|agent)-[0-9]+`),
			regexp.MustCompile(`flowlens-(server|agent):[^[:space:]]+`),
		} {
			if forbidden.MatchString(contents) {
				t.Errorf("%s contains a FlowLens runtime container deployment reference", name)
			}
		}
	}
}

func TestNativeLifecycleScriptContracts(t *testing.T) {
	backup := readScript(t, "backup.sh")
	for _, fragment := range []string{"FLOWLENS_DATABASE_URL", "FLOWLENS_PG_DUMP", "pg_dump", "--format=custom"} {
		if !strings.Contains(backup, fragment) {
			t.Errorf("backup.sh must contain %q", fragment)
		}
	}
	for _, forbidden := range []string{"FLOWLENS_POSTGRES_CONTAINER", "git_commit", "postgres_container"} {
		if strings.Contains(backup, forbidden) {
			t.Errorf("backup.sh manifest must not depend on %q", forbidden)
		}
	}

	restore := readScript(t, "restore.sh")
	for _, fragment := range []string{"FLOWLENS_DATABASE_URL", "FLOWLENS_PG_RESTORE", "FLOWLENS_PSQL", `"$systemctl" is-active --quiet flowlens-server.service`, `"$systemctl" is-active --quiet flowlens-agent.service`, `"$systemctl" start flowlens-server.service`, `"$systemctl" start flowlens-agent.service`, "/healthz"} {
		if !strings.Contains(restore, fragment) {
			t.Errorf("restore.sh must contain %q", fragment)
		}
	}

	uninstall := readScript(t, "uninstall.sh")
	for _, fragment := range []string{"flowlens-server.service", "flowlens-agent.service", "/usr/local/bin/flowlens-server", "/usr/local/bin/flowlens-agent", "/opt/flowlens/web", "/etc/flowlens"} {
		if !strings.Contains(uninstall, fragment) {
			t.Errorf("uninstall.sh must remove %q", fragment)
		}
	}
	for _, preserved := range []string{"/var/lib/flowlens", "/var/lib/flowlens-agent"} {
		if strings.Contains(uninstall, "rm -rf "+preserved) {
			t.Errorf("uninstall.sh must preserve %s by default", preserved)
		}
	}
}

func TestAcceptanceScriptCollectsRequiredReleaseEvidence(t *testing.T) {
	contents := readScript(t, "acceptance.sh")
	for _, fragment := range []string{
		"FLOWLENS_ADMIN_PASSWORD_FILE",
		"FLOWLENS_TEST_DATABASE_URL",
		"FLOWLENS_QUERY_PLAN_ROWS=1000000",
		"FLOWLENS_ACCURACY_DURATION_SECONDS:-600",
		"FLOWLENS_ACCURACY_LIMIT_PERCENT:-2",
		"FLOWLENS_CONCURRENT_CONNECTIONS:-1000",
		"/api/v1/session",
		"sha256sum",
		"verify-attribution.sh",
		"systemctl",
		"FLOWLENS_POSTGRES_ADAPTER",
		"createdb",
		"dropdb",
		"query_latency",
		"ingestion_lag",
		"accuracy_error",
		"https://speed.cloudflare.com/__down?bytes=1048576",
		"https://speed.cloudflare.com/__up",
		"--data-binary @",
		"systemctl restart flowlens-server",
		"systemctl restart flowlens-agent",
		"systemctl show flowlens-server -p MemoryCurrent",
		"sha256sum /usr/local/bin/flowlens-server",
		"connections_ready",
		"FLOWLENS_ACCEPTANCE_LOCAL_URL=$local_url",
		"playwright",
		"internal/server/retention",
		"internal/server/trafficquery",
		"internal/server/webhook",
	} {
		if !strings.Contains(contents, fragment) {
			t.Fatalf("acceptance.sh must contain %q", fragment)
		}
	}
	if strings.Contains(contents, "/api/v1/auth/session") {
		t.Fatal("acceptance.sh must use the registered session endpoint")
	}
	if strings.Contains(contents, "flowlens-agent\" --version") {
		t.Fatal("acceptance.sh must not execute an unsupported agent version flag")
	}
	if !strings.Contains(contents, "FLOWLENS_URL=$public_url") {
		t.Fatal("acceptance.sh must run authenticated attribution against the cookie's public origin")
	}
	if strings.Contains(contents, "cat $FLOWLENS_ADMIN_PASSWORD_FILE") {
		t.Fatal("acceptance.sh must quote the password file path")
	}
	if strings.Contains(contents, `socket.create_connection(("127.0.0.1", 8088)`) {
		t.Fatal("acceptance.sh connection load must honor FLOWLENS_LOCAL_URL")
	}
	if strings.Contains(contents, "docker restart") {
		t.Fatal("acceptance.sh must restart FlowLens through systemd")
	}
	for _, required := range []string{
		"FLOWLENS_ADMIN_PASSWORD_FILE:-/etc/flowlens/admin-password",
		"FLOWLENS_ACCEPTANCE_REPORT:-/var/lib/flowlens/acceptance-report.md",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("acceptance.sh must contain safe default %q", required)
		}
	}

	for _, name := range []string{"acceptance.sh", "backup.sh", "configure-deploy.sh", "restore.sh", "uninstall.sh", "verify-attribution.sh"} {
		command := exec.Command("sh", "-n", filepath.Join(name))
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s syntax: %v\n%s", name, err, output)
		}
	}
}

func TestAcceptanceReportIsMeasurementFreeTemplate(t *testing.T) {
	contents := readFile(t, "../docs/operations/acceptance-report.md")
	for _, required := range []string{"TEMPLATE", "No acceptance run has been recorded", "Pending acceptance run"} {
		if !strings.Contains(contents, required) {
			t.Errorf("acceptance report template must contain %q", required)
		}
	}
	for _, executed := range []*regexp.Regexp{
		regexp.MustCompile(`Generated:[[:space:]]+[0-9]{4}-[0-9]{2}-[0-9]{2}`),
		regexp.MustCompile(`Release revision:[[:space:]]+` + "`?" + `[[:xdigit:]]{7,}`),
	} {
		if executed.MatchString(contents) {
			t.Error("acceptance report template contains executed release evidence")
		}
	}
}

func TestPublishableTreeExcludesInternalPlans(t *testing.T) {
	command := exec.Command("git", "-C", "..", "ls-files", "docs/superpowers")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 0 {
		t.Error("publishable tree must not track internal plans or specifications")
	}
}

func TestPrivacyCheckScansTrackedFilesWithExternalDenylist(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init", "--quiet")
	runGit(t, repository, "config", "user.name", "Privacy Test")
	runGit(t, repository, "config", "user.email", "privacy@example.invalid")

	fixture := filepath.Join(repository, "nested", "fixture.txt")
	if err := os.MkdirAll(filepath.Dir(fixture), 0755); err != nil {
		t.Fatal(err)
	}
	privateValue := "synthetic-node.private.invalid"
	denylist := filepath.Join(t.TempDir(), "denylist")
	if err := os.WriteFile(denylist, []byte(privateValue+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(fixture, []byte(privateValue+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "nested/fixture.txt")
	if err := os.WriteFile(fixture, []byte("unstaged public replacement\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output := runPrivacyCheck(t, repository, denylist, false)
	if strings.Contains(output, privateValue) {
		t.Error("privacy check must not echo a denied value")
	}
	if err := os.Remove(fixture); err != nil {
		t.Fatal(err)
	}
	runPrivacyCheck(t, repository, denylist, false)

	if err := os.WriteFile(fixture, []byte("staged public replacement\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "nested/fixture.txt")
	runPrivacyCheck(t, repository, denylist, true)

	binaryFixture := filepath.Join(repository, "nested", "fixture.bin")
	if err := os.WriteFile(binaryFixture, append([]byte{0, 1}, append([]byte(privateValue), 0)...), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "nested/fixture.bin")
	binaryOutput := runPrivacyCheck(t, repository, denylist, false)
	if strings.Contains(binaryOutput, privateValue) {
		t.Error("privacy check must not echo a denied binary value")
	}
	if err := os.WriteFile(binaryFixture, []byte{0, 1, 2}, 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "nested/fixture.bin")
	runPrivacyCheck(t, repository, denylist, true)

	rootMarker := filepath.Join(repository, "root-marker.txt")
	if err := os.WriteFile(rootMarker, []byte(privateValue+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "root-marker.txt")
	runPrivacyCheck(t, filepath.Join(repository, "nested"), denylist, false)
	if err := os.WriteFile(rootMarker, []byte("public root marker\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "root-marker.txt")

	trackedDenylist := filepath.Join(repository, "denylist.local")
	if err := os.WriteFile(trackedDenylist, []byte("another-synthetic-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "denylist.local")
	runPrivacyCheck(t, repository, trackedDenylist, false)
}

func TestPublishedTreePassesStructuralPrivacyCheck(t *testing.T) {
	runPrivacyCheck(t, "..", "", true)
}

func TestPublicReleaseFilesExist(t *testing.T) {
	for _, name := range []string{
		"README.md",
		"LICENSE",
		"NOTICE",
		"SECURITY.md",
		"CONTRIBUTING.md",
		filepath.Join(".github", "workflows", "ci.yml"),
	} {
		if info, err := os.Stat(filepath.Join("..", name)); err != nil {
			t.Errorf("public release file %s: %v", name, err)
		} else if !info.Mode().IsRegular() {
			t.Errorf("public release file %s must be a regular file", name)
		}
	}
}

func TestApacheReleaseMetadata(t *testing.T) {
	license := readFile(t, "../LICENSE")
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(license)))
	requireEqual(t, digest, "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30", "canonical Apache-2.0 LICENSE SHA-256")
	for _, required := range []string{
		"Apache License\n                           Version 2.0, January 2004",
		"http://www.apache.org/licenses/",
		"TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION",
		"END OF TERMS AND CONDITIONS",
		"Copyright [yyyy] [name of copyright owner]",
	} {
		if !strings.Contains(license, required) {
			t.Errorf("LICENSE must contain official Apache-2.0 text %q", required)
		}
	}
	if len(license) < 11000 {
		t.Error("LICENSE is too short to contain the full Apache License 2.0 text")
	}

	notice := readFile(t, "../NOTICE")
	for _, required := range []string{"FlowLens", "Copyright 2026 FlowLens contributors", "SPDX-License-Identifier: Apache-2.0"} {
		if !strings.Contains(notice, required) {
			t.Errorf("NOTICE must contain %q", required)
		}
	}
	if strings.Contains(notice, "@") || regexp.MustCompile(`(?i)https?://[^[:space:]]*(github|linkedin)`).MatchString(notice) || regexp.MustCompile(`Copyright 2026 [A-Z][a-z]+ [A-Z][a-z]+`).MatchString(notice) {
		t.Error("NOTICE must not publish a personal URL, email, or legal identity")
	}
}

func TestPublicSecurityAndContributionContracts(t *testing.T) {
	security := readFile(t, "../SECURITY.md")
	for _, required := range []string{"GitHub Private Vulnerability Reporting", "Security tab", "Do not include secrets", "public issue"} {
		if !strings.Contains(security, required) {
			t.Errorf("SECURITY.md must contain %q", required)
		}
	}
	if strings.Contains(security, "@") {
		t.Error("SECURITY.md must not publish a personal email address")
	}

	contributing := readFile(t, "../CONTRIBUTING.md")
	for _, command := range []string{
		"go test ./...",
		"go vet ./...",
		"cd web && npm ci",
		"cd web && npm test",
		"cd web && npm run build",
		"sh scripts/privacy-check.sh",
		"sh -n scripts/*.sh",
		"shellcheck scripts/*.sh",
		"docker run --detach --rm --name flowlens-integration-postgres -e POSTGRES_USER=flowlens_ci -e POSTGRES_PASSWORD=flowlens_ci -e POSTGRES_DB=postgres -p 127.0.0.1:55432:5432 postgres:17",
		"FLOWLENS_POSTGRES_ADMIN_URL='postgres://flowlens_ci:flowlens_ci@127.0.0.1:55432/postgres?sslmode=disable' sh scripts/ci-integration.sh",
	} {
		if !strings.Contains(contributing, "`"+command+"`") {
			t.Errorf("CONTRIBUTING.md must document exact command %q", command)
		}
	}
	for _, warning := range []string{"test-only PostgreSQL 17 container", "not a FlowLens deployment", "`go test ./...` skips PostgreSQL integration tests unless their database environment is configured"} {
		if !strings.Contains(contributing, warning) {
			t.Errorf("CONTRIBUTING.md must contain %q", warning)
		}
	}
}

func TestReadmeDocumentsReleaseBoundaries(t *testing.T) {
	readme := readFile(t, "../README.md")
	for _, required := range []string{
		"Apache-2.0",
		"native systemd services",
		"PostgreSQL",
		"eBPF",
		"docs/operations/install.md",
		"docs/operations/attribution.md",
		"Docker socket access is effectively host-level privilege",
		"Limitations",
		"synthetic data",
		"Do not publish screenshots containing real infrastructure",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("README.md must contain %q", required)
		}
	}
	for _, forbidden := range []string{"docker " + "compose", "measured throughput", "guaranteed", "/" + "root/", "@gmail.com"} {
		if strings.Contains(strings.ToLower(readme), strings.ToLower(forbidden)) {
			t.Errorf("README.md must not contain %q", forbidden)
		}
	}
}

func TestCIWorkflowContract(t *testing.T) {
	workflow := readFile(t, "../.github/workflows/ci.yml")
	config := ciWorkflow{}
	if err := yaml.Unmarshal([]byte(workflow), &config); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}
	if len(config.Permissions) != 1 || config.Permissions["contents"] != "read" {
		t.Errorf("ci.yml permissions must be exactly contents: read; got %#v", config.Permissions)
	}
	job, ok := config.Jobs["verify"]
	if !ok {
		t.Fatal("ci.yml must define the verify job")
	}
	postgres, ok := job.Services["postgres"]
	if !ok || postgres.Image != "postgres:17" {
		t.Errorf("ci.yml PostgreSQL service image must be postgres:17; got %q", postgres.Image)
	}
	if !strings.Contains(postgres.Options, "--health-cmd pg_isready") {
		t.Error("ci.yml PostgreSQL service must have a pg_isready health check")
	}

	uses := map[string]ciStep{}
	runs := map[string]ciStep{}
	for _, step := range job.Steps {
		uses[step.Uses] = step
		runs[step.Run] = step
	}
	for _, action := range []string{"actions/checkout@v4", "actions/setup-go@v5", "actions/setup-node@v4"} {
		if _, ok := uses[action]; !ok {
			t.Errorf("ci.yml must use %s", action)
		}
	}
	setupNode := uses["actions/setup-node@v4"]
	if setupNode.With["cache"] != "npm" || setupNode.With["cache-dependency-path"] != "web/package-lock.json" {
		t.Errorf("setup-node must cache npm with web/package-lock.json; got %#v", setupNode.With)
	}
	for _, command := range []string{"go test ./...", "go vet ./...", "npm ci", "npm test", "npm run build", "sh scripts/ci-integration.sh", "sh scripts/privacy-check.sh"} {
		if _, ok := runs[command]; !ok {
			t.Errorf("ci.yml must run exact command %q", command)
		}
	}
	integration := runs["sh scripts/ci-integration.sh"]
	if integration.Env["FLOWLENS_POSTGRES_ADMIN_URL"] == "" {
		t.Error("ci.yml integration step must pass FLOWLENS_POSTGRES_ADMIN_URL through the environment")
	}
	if strings.Contains(workflow, "${{ secrets.") || strings.Contains(workflow, "docker "+"compose") {
		t.Error("ci.yml must not use repository secrets or a FlowLens container deployment")
	}
	if !strings.Contains(workflow, "sh -n scripts/*.sh") || !strings.Contains(workflow, "shellcheck scripts/*.sh") {
		t.Error("ci.yml must run shell syntax and ShellCheck checks")
	}
}

type ciWorkflow struct {
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]ciJob  `yaml:"jobs"`
}

type ciJob struct {
	Services map[string]ciService `yaml:"services"`
	Steps    []ciStep             `yaml:"steps"`
}

type ciService struct {
	Image   string `yaml:"image"`
	Options string `yaml:"options"`
}

type ciStep struct {
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}

func TestCIIntegrationScriptContract(t *testing.T) {
	contents := readScript(t, "ci-integration.sh")
	for _, required := range []string{
		"FLOWLENS_POSTGRES_ADMIN_URL",
		"createdb",
		"dropdb",
		"trap cleanup EXIT",
		"trap 'exit 1' HUP INT TERM",
		"FLOWLENS_TEST_DATABASE_URL",
		"for package in auth retention trafficquery webhook",
		`go test "./internal/server/$package"`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("ci-integration.sh must contain %q", required)
		}
	}
	for _, forbidden := range []string{"set -x", "echo $FLOWLENS_POSTGRES_ADMIN_URL", "printf '%s\\n' \"$FLOWLENS_POSTGRES_ADMIN_URL\""} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("ci-integration.sh must not expose its database URL with %q", forbidden)
		}
	}
	command := exec.Command("sh", "-n", "ci-integration.sh")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ci-integration.sh syntax: %v\n%s", err, output)
	}
}

func TestCIIntegrationScriptIsolatesPackagesAndCleansUp(t *testing.T) {
	tools := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	libpqChecks := `[ "$PGHOST" = 127.0.0.1 ] && [ "$PGPORT" = 5432 ] && [ "$PGUSER" = flowlens_ci ] && [ "$PGPASSWORD" = database-value ] && [ "$PGDATABASE" = postgres ] && [ "$PGSSLMODE" = disable ] || exit 90
case "$*" in *postgres://*|*database-value*) exit 91;; esac
`
	writeExecutable(t, filepath.Join(tools, "createdb"), "#!/bin/sh\n"+libpqChecks+"for argument do database=$argument; done\nprintf 'create|%s\\n' \"$database\" >>\"$FLOWLENS_CI_TEST_LOG\"\n")
	writeExecutable(t, filepath.Join(tools, "dropdb"), "#!/bin/sh\n"+libpqChecks+"for argument do database=$argument; done\nprintf 'drop|%s\\n' \"$database\" >>\"$FLOWLENS_CI_TEST_LOG\"\n")
	writeExecutable(t, filepath.Join(tools, "go"), "#!/bin/sh\nprintf 'test|%s|%s\\n' \"$*\" \"$FLOWLENS_TEST_DATABASE_URL\" >>\"$FLOWLENS_CI_TEST_LOG\"\n")

	adminURL := "postgres://flowlens_ci:database-value@127.0.0.1:5432/postgres?sslmode=disable"
	command := exec.Command("sh", "ci-integration.sh")
	command.Env = append(os.Environ(),
		"PATH="+tools+":"+os.Getenv("PATH"),
		"FLOWLENS_CI_TEST_LOG="+logPath,
		"FLOWLENS_POSTGRES_ADMIN_URL="+adminURL,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ci-integration.sh: %v\n%s", err, output)
	}
	if strings.Contains(string(output), adminURL) {
		t.Fatal("ci-integration.sh exposed its admin URL")
	}

	lines := strings.Split(strings.TrimSpace(readFile(t, logPath)), "\n")
	created := map[string]bool{}
	dropped := map[string]bool{}
	tested := map[string]bool{}
	for _, line := range lines {
		fields := strings.SplitN(line, "|", 3)
		switch fields[0] {
		case "create":
			created[fields[1]] = true
		case "drop":
			dropped[fields[1]] = true
		case "test":
			tested[fields[1]] = true
			matchedDatabase := false
			for database := range created {
				if strings.Contains(fields[2], "/"+database+"?") {
					matchedDatabase = true
				}
			}
			if !matchedDatabase {
				t.Errorf("integration URL does not name an isolated created database")
			}
		}
	}
	if len(created) != 4 || len(dropped) != 4 {
		t.Fatalf("expected four created and dropped databases; created=%d dropped=%d", len(created), len(dropped))
	}
	for database := range created {
		if !dropped[database] {
			t.Errorf("database %s was not cleaned up", database)
		}
	}
	for _, pkg := range []string{"test ./internal/server/auth", "test ./internal/server/retention", "test ./internal/server/trafficquery", "test ./internal/server/webhook"} {
		if !tested[pkg] {
			t.Errorf("integration script did not run exact package command go %s", pkg)
		}
	}
}

func TestCIIntegrationScriptCleansUpAmbiguousCreateFailure(t *testing.T) {
	tools := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	writeExecutable(t, filepath.Join(tools, "createdb"), "#!/bin/sh\nfor argument do database=$argument; done\nprintf 'create|%s\\n' \"$database\" >>\"$FLOWLENS_CI_TEST_LOG\"\nexit 88\n")
	writeExecutable(t, filepath.Join(tools, "dropdb"), "#!/bin/sh\nfor argument do database=$argument; done\nprintf 'drop|%s\\n' \"$database\" >>\"$FLOWLENS_CI_TEST_LOG\"\n")
	writeExecutable(t, filepath.Join(tools, "go"), "#!/bin/sh\nexit 89\n")

	command := exec.Command("sh", "ci-integration.sh")
	command.Env = append(os.Environ(),
		"PATH="+tools+":"+os.Getenv("PATH"),
		"FLOWLENS_CI_TEST_LOG="+logPath,
		"FLOWLENS_POSTGRES_ADMIN_URL=postgres://flowlens_ci:database-value@127.0.0.1:5432/postgres?sslmode=disable",
	)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("ci-integration.sh unexpectedly passed an ambiguous create failure: %s", output)
	}
	lines := strings.Split(strings.TrimSpace(readFile(t, logPath)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "create|") || strings.Replace(lines[0], "create|", "drop|", 1) != lines[1] {
		t.Fatalf("ci-integration.sh did not clean up the possibly created database: %q", lines)
	}
}

func TestCIIntegrationScriptRejectsMalformedURLWithoutInheritedPGFallback(t *testing.T) {
	tools := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	for _, name := range []string{"createdb", "dropdb", "go"} {
		writeExecutable(t, filepath.Join(tools, name), "#!/bin/sh\nprintf '%s\\n' "+name+" >>\"$FLOWLENS_CI_TEST_LOG\"\n")
	}

	adminURL := "mysql://flowlens_ci:url-secret@127.0.0.1:5432/postgres?sslmode=disable"
	command := exec.Command("sh", "ci-integration.sh")
	command.Env = append(os.Environ(),
		"PATH="+tools+":"+os.Getenv("PATH"),
		"FLOWLENS_CI_TEST_LOG="+logPath,
		"FLOWLENS_POSTGRES_ADMIN_URL="+adminURL,
		"PGHOST=fallback.invalid",
		"PGPORT=6543",
		"PGUSER=fallback-user",
		"PGPASSWORD=inherited-secret",
		"PGDATABASE=fallback-database",
		"PGSSLMODE=require",
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("ci-integration.sh accepted a malformed admin URL: %s", output)
	}
	message := strings.TrimSpace(string(output))
	if message != "FLOWLENS_POSTGRES_ADMIN_URL must be a PostgreSQL URL with a host and user" {
		t.Fatalf("unexpected malformed URL error: %q", message)
	}
	for _, secret := range []string{adminURL, "url-secret", "inherited-secret"} {
		if strings.Contains(string(output), secret) {
			t.Fatal("ci-integration.sh exposed a database secret after URL validation failed")
		}
	}
	if contents, readErr := os.ReadFile(logPath); readErr == nil && len(contents) != 0 {
		t.Fatalf("ci-integration.sh called tools after URL validation failed: %s", contents)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
}

func requireEqual(t *testing.T, got, want, description string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q want %q", description, got, want)
	}
}

func TestGitignoreProtectsLocalAndGeneratedArtifacts(t *testing.T) {
	gitignore := readFile(t, "../.gitignore")
	for _, pattern := range []string{
		".env",
		"*.db",
		"*.sql",
		"!/migrations/*.sql",
		"*.dump",
		"*.backup",
		"GeoLite2-*.mmdb",
		"/state/",
		"/web/node_modules/",
		"/web/dist/",
		"/web/test-results/",
		"/web/playwright-report/",
		"/.flowlens-privacy-denylist",
	} {
		if !strings.Contains(gitignore, pattern) {
			t.Errorf(".gitignore must contain %q", pattern)
		}
	}
}

func TestPrivacyCheckAppliesStructuralRulesToStagedPaths(t *testing.T) {
	paths := []string{
		filepath.Join("archive", "root", "private.txt"),
		filepath.Join("archive", "home", "operator", "private.txt"),
		filepath.Join("fixtures", "flowlens-"+"server-42.txt"),
		filepath.Join("fixtures", "flowlens-"+"agent:legacy.txt"),
		filepath.Join("fixtures", "docker"+" compose.yml"),
	}
	for index, badPath := range paths {
		t.Run(filepath.Base(badPath), func(t *testing.T) {
			repository := t.TempDir()
			runGit(t, repository, "init", "--quiet")
			absoluteBadPath := filepath.Join(repository, badPath)
			if err := os.MkdirAll(filepath.Dir(absoluteBadPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(absoluteBadPath, []byte("public fixture\n"), 0644); err != nil {
				t.Fatal(err)
			}
			runGit(t, repository, "add", badPath)
			output := runPrivacyCheck(t, repository, "", false)
			if strings.Contains(output, badPath) {
				t.Error("privacy check must not echo a structurally denied path")
			}

			safePath := filepath.Join(repository, "safe", fmt.Sprintf("renamed-%d.txt", index))
			if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(absoluteBadPath, safePath); err != nil {
				t.Fatal(err)
			}
			runPrivacyCheck(t, repository, "", false)
			runGit(t, repository, "add", "--all")
			runPrivacyCheck(t, repository, "", true)
		})
	}
}

func TestNativeOperationsDocumentation(t *testing.T) {
	install := readFile(t, "../docs/operations/install.md")
	for _, required := range []string{
		"monitor.example.com",
		"flowlens-node-1",
		"existing PostgreSQL",
		"systemctl",
		"/healthz",
		"Nginx Proxy Manager",
		"host-gateway",
		"FLOWLENS_DOCKER_ATTRIBUTION",
	} {
		if !strings.Contains(install, required) {
			t.Errorf("install.md must document %q", required)
		}
	}
	for _, forbidden := range []string{"docker " + "compose", "Docker deployment", "FlowLens container"} {
		if strings.Contains(strings.ToLower(install), strings.ToLower(forbidden)) {
			t.Errorf("install.md must not describe FlowLens container deployment with %q", forbidden)
		}
	}

	agentEnvironment := readFile(t, "../deploy/agent.env.example")
	for _, required := range []string{"FLOWLENS_NODE_ID=flowlens-node-1", "FLOWLENS_NPM_LOG_GLOBS=/data/logs/proxy-host-*_access.log", "FLOWLENS_DOCKER_ATTRIBUTION=disabled"} {
		if !strings.Contains(agentEnvironment, required) {
			t.Errorf("agent.env.example must contain %q", required)
		}
	}
	if strings.Contains(agentEnvironment, "FLOWLENS_CAPTURE_INTERFACES=enp") {
		t.Error("agent.env.example must not assume a host interface name")
	}

	report := readFile(t, "../docs/operations/acceptance-report.md")
	for _, misleading := range []string{"Generated: 202", "Release revision: `", "PASS", "verification " + "passed"} {
		if strings.Contains(report, misleading) {
			t.Errorf("acceptance report template must not masquerade as executed evidence with %q", misleading)
		}
	}
}

func TestPublicScreenshotUsesSyntheticData(t *testing.T) {
	for _, path := range []string{
		"../web/playwright.screenshot.config.ts",
		"../web/screenshot/public-overview.spec.ts",
		"../docs/images/flowlens-overview-concept.png",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("public screenshot deliverable is missing: %s", path)
		}
	}

	readme := readFile(t, "../README.md")
	for _, required := range []string{"docs/images/flowlens-overview-concept.png", "synthetic data"} {
		if !strings.Contains(readme, required) {
			t.Errorf("README must describe the synthetic public screenshot with %q", required)
		}
	}

	spec := readFile(t, "../web/screenshot/public-overview.spec.ts")
	for _, required := range []string{"example.com", "example.net", "203.0.113."} {
		if !strings.Contains(spec, required) {
			t.Errorf("public screenshot fixture must use reserved synthetic data %q", required)
		}
	}
}

func TestReadmesProvideBilingualNavigation(t *testing.T) {
	english := readFile(t, "../README.md")
	chinese := readFile(t, "../README.zh-CN.md")
	for _, required := range []string{"[简体中文](README.zh-CN.md)", "docs/images/flowlens-overview-concept.png"} {
		if !strings.Contains(english, required) {
			t.Errorf("English README must contain %q", required)
		}
	}
	for _, required := range []string{"[English](README.md)", "概念图", "合成数据", "不得发布包含真实基础设施"} {
		if !strings.Contains(chinese, required) {
			t.Errorf("Chinese README must contain %q", required)
		}
	}
}

func TestWebBundleIncludesVersionedFlowLensFavicon(t *testing.T) {
	html := readFile(t, "../web/index.html")
	for _, required := range []string{`rel="icon"`, `type="image/svg+xml"`, `href="/favicon.svg?v=1"`} {
		if !strings.Contains(html, required) {
			t.Errorf("web index must contain favicon attribute %q", required)
		}
	}

	icon := readFile(t, "../web/public/favicon.svg")
	for _, required := range []string{`viewBox="0 0 32 32"`, "#20312f", "#3e5a57", "#62d0a2", "M4 16"} {
		if !strings.Contains(icon, required) {
			t.Errorf("favicon must contain FlowLens brand fragment %q", required)
		}
	}
}

func TestAgentServiceHasNoHostSpecificNPMPath(t *testing.T) {
	service := readFile(t, "../deploy/flowlens-agent.service")
	if strings.Contains(service, "/"+"root/") {
		t.Error("flowlens-agent.service must not publish a root-home path")
	}

	attribution := readFile(t, "../docs/operations/attribution.md")
	for _, required := range []string{"systemctl edit flowlens-agent.service", "[Service]", "BindReadOnlyPaths=/path/to/npm/logs:/data/logs", "[Sent-to $server:$port]"} {
		if !strings.Contains(attribution, required) {
			t.Errorf("attribution.md must show generic NPM drop-in guidance %q", required)
		}
	}
}

func TestAttributionVerifierUsesSystemDNS(t *testing.T) {
	contents := readScript(t, "verify-attribution.sh")
	if strings.Contains(contents, "--add-host") {
		t.Error("verify-attribution.sh must not pin a public hostname to an address")
	}
	if regexp.MustCompile(`(?m)^[[:space:]]*nslookup[[:space:]]+[^[:space:]]+[[:space:]]+[^>|[:space:]]+`).MatchString(contents) {
		t.Error("verify-attribution.sh must use the system resolver")
	}
}

func TestBackupRestoreDocumentationPreservesRootReadableConfiguration(t *testing.T) {
	contents := readFile(t, "../docs/operations/backup-restore.md")
	for _, required := range []string{
		"sudo env FLOWLENS_SERVER_ENVIRONMENT=/etc/flowlens/server.env",
		"FLOWLENS_SERVER_ENVIRONMENT=/etc/flowlens/restore-target.env",
		"FLOWLENS_POSTGRES_ADMIN_URL",
		"is used only to create the empty target database",
		"Avoid putting a database URL directly in a command",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("backup-restore.md must contain %q", required)
		}
	}
}

func runGit(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git fixture command failed: %v\n%s", err, output)
	}
}

func runPrivacyCheck(t *testing.T, repository, denylist string, wantSuccess bool) string {
	t.Helper()
	command := exec.Command("sh", "privacy-check.sh")
	command.Env = append(os.Environ(), "FLOWLENS_PRIVACY_ROOT="+repository)
	if denylist != "" {
		command.Env = append(command.Env, "FLOWLENS_PRIVACY_DENYLIST_FILE="+denylist)
	}
	output, err := command.CombinedOutput()
	if wantSuccess && err != nil {
		t.Fatalf("privacy check failed: %v\n%s", err, output)
	}
	if !wantSuccess && err == nil {
		t.Fatal("privacy check unexpectedly passed")
	}
	return string(output)
}

func TestNativeServerServiceIsHardened(t *testing.T) {
	contents := readFile(t, "../deploy/flowlens-server.service")
	for _, fragment := range []string{
		"After=network-online.target",
		"Wants=network-online.target",
		"Type=simple",
		"User=flowlens",
		"Group=flowlens",
		"EnvironmentFile=/etc/flowlens/server.env",
		"ExecStart=/usr/local/bin/flowlens-server",
		"Restart=on-failure",
		"MemoryMax=384M",
		"TasksMax=128",
		"UMask=0027",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
		"ProtectSystem=strict",
		"ProtectHome=true",
		"ProtectKernelTunables=true",
		"ProtectKernelModules=true",
		"ProtectControlGroups=true",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
		"ReadWritePaths=/var/lib/flowlens",
	} {
		if !strings.Contains(contents, fragment) {
			t.Errorf("flowlens-server.service must contain %q", fragment)
		}
	}
}

func TestNativeServerInstallerContract(t *testing.T) {
	contents := readScript(t, "install-server.sh")
	for _, fragment := range []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"export PATH",
		"id -u",
		"useradd --system",
		"/usr/local/bin/flowlens-server",
		"/opt/flowlens/web",
		"/etc/flowlens/server.env",
		"/var/lib/flowlens",
		"/var/lib/flowlens/geoip",
		"validate_environment",
		"FLOWLENS_DATABASE_URL",
		"FLOWLENS_AGENT_TOKEN",
		"FLOWLENS_BOOTSTRAP_TOKEN",
		"FLOWLENS_SECRET_KEY",
		"FLOWLENS_PUBLIC_URL",
		"FLOWLENS_INSTALL_ROOT",
		"state_owner=flowlens",
		"state_group=flowlens",
		"systemctl daemon-reload",
		"systemctl enable flowlens-server.service",
		"FLOWLENS_INSTALL_START",
		"systemctl restart flowlens-server.service",
	} {
		if !strings.Contains(contents, fragment) {
			t.Errorf("install-server.sh must contain %q", fragment)
		}
	}
	for _, unsafe := range []string{"set -x", `cat "$environment"`} {
		if strings.Contains(contents, unsafe) {
			t.Errorf("install-server.sh must not expose configuration with %q", unsafe)
		}
	}

	command := exec.Command("sh", "-n", "install-server.sh")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install-server.sh syntax: %v\n%s", err, output)
	}
}

func TestNativeServerEnvironmentAndMakeTargets(t *testing.T) {
	environment := readFile(t, "../deploy/server.env.example")
	for _, fragment := range []string{
		"FLOWLENS_DATABASE_URL=postgres://flowlens:CHANGE_DB_PASSWORD@127.0.0.1:5432/flowlens?sslmode=disable",
		"FLOWLENS_PUBLIC_URL=https://monitor.example.com",
		"FLOWLENS_LISTEN_ADDRESS=127.0.0.1:8088",
		"FLOWLENS_WEB_DIR=/opt/flowlens/web",
		"CHANGE_DB_PASSWORD",
		"CHANGE_AGENT_TOKEN",
		"CHANGE_BOOTSTRAP_TOKEN",
		"CHANGE_SECRET_KEY",
	} {
		if !strings.Contains(environment, fragment) {
			t.Errorf("server.env.example must contain %q", fragment)
		}
	}

	makefile := readFile(t, "../Makefile")
	for _, fragment := range []string{
		"bin/flowlens-server",
		"bin/flowlens-agent",
		"web/dist",
		"install-server:",
		"install-agent:",
	} {
		if !strings.Contains(makefile, fragment) {
			t.Errorf("Makefile must contain %q", fragment)
		}
	}
}

func TestNativeServerInstallerRejectsTrailingPlaceholderAssignment(t *testing.T) {
	requireInstallerTestRoot(t)
	for _, assignment := range []string{
		"FLOWLENS_AGENT_TOKEN=CHANGE_AGENT_TOKEN",
		`FLOWLENS_AGENT_TOKEN=""`,
		"FLOWLENS_AGENT_TOKEN=''",
	} {
		t.Run(assignment, func(t *testing.T) {
			harness := newServerInstallHarness(t)
			environment := readFile(t, harness.environment)
			environment += assignment + "\n"
			if err := os.WriteFile(harness.environment, []byte(environment), 0600); err != nil {
				t.Fatal(err)
			}

			output, err := harness.run("")
			if err == nil {
				t.Fatalf("installer accepted invalid final assignment %q; output: %s", assignment, output)
			}
			if strings.Contains(output, harness.agentToken) {
				t.Fatal("installer exposed the superseded agent token")
			}
			if !strings.Contains(output, "FLOWLENS_AGENT_TOKEN") {
				t.Fatalf("installer did not identify the invalid key: %s", output)
			}
		})
	}
}

func TestNativeServerInstallerStagesIdempotentlyWithoutHostActions(t *testing.T) {
	requireInstallerTestRoot(t)
	harness := newServerInstallHarness(t)

	output, err := harness.run("")
	if err != nil {
		t.Fatalf("first install failed: %v\n%s", err, output)
	}
	harness.assertNoSecrets(t, output)
	harness.assertInstalledFiles(t, "first bundle")
	assertLogLines(t, harness.hostActionLog, nil)

	stale := filepath.Join(harness.root, "opt/flowlens/web/stale.js")
	if err := os.WriteFile(stale, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harness.webBundle, "index.html"), []byte("second bundle"), 0644); err != nil {
		t.Fatal(err)
	}
	truncateFile(t, harness.hostActionLog)

	output, err = harness.run("false")
	if err != nil {
		t.Fatalf("second install failed: %v\n%s", err, output)
	}
	harness.assertNoSecrets(t, output)
	harness.assertInstalledFiles(t, "second bundle")
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale web asset was not removed: %v", err)
	}
	assertLogLines(t, harness.hostActionLog, nil)
}

func TestNativeServerInstallerStagesAliasedSourcesWithoutDataLoss(t *testing.T) {
	requireInstallerTestRoot(t)
	harness := newServerInstallHarness(t)
	output, err := harness.run("false")
	if err != nil {
		t.Fatalf("initial staging install failed: %v\n%s", err, output)
	}

	binary := filepath.Join(harness.root, "usr/local/bin/flowlens-server")
	webBundle := filepath.Join(harness.root, "opt/flowlens/web")
	environment := filepath.Join(harness.root, "etc/flowlens/server.env")
	command := harness.command(binary, webBundle, environment, "false")
	outputBytes, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("aliased staging install failed: %v\n%s", err, outputBytes)
	}
	harness.assertNoSecrets(t, string(outputBytes))
	harness.assertInstalledFiles(t, "first bundle")
	assertLogLines(t, harness.hostActionLog, nil)
}

func TestNativeServerInstallerStagesOverlappingWebSourceWithoutDataLoss(t *testing.T) {
	requireInstallerTestRoot(t)
	harness := newServerInstallHarness(t)
	if err := os.WriteFile(filepath.Join(harness.root, "index.html"), []byte("overlap bundle"), 0644); err != nil {
		t.Fatal(err)
	}

	command := harness.command(harness.binary, harness.root, harness.environment, "false")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("overlapping web staging failed: %v\n%s", err, output)
	}
	assertFile(t, filepath.Join(harness.root, "opt/flowlens/web/index.html"), 0644, "overlap bundle")
	assertLogLines(t, harness.hostActionLog, nil)
}

func TestNativeServerInstallerRejectsDestinationSymlinkEscape(t *testing.T) {
	requireInstallerTestRoot(t)
	harness := newServerInstallHarness(t)
	outside := filepath.Join(harness.base, "outside-root")
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(harness.root, "opt")); err != nil {
		t.Fatal(err)
	}

	output, err := harness.run("false")
	if err == nil {
		t.Fatalf("installer accepted a destination outside FLOWLENS_INSTALL_ROOT: %s", output)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "flowlens")); !os.IsNotExist(statErr) {
		t.Fatalf("installer wrote outside FLOWLENS_INSTALL_ROOT: %v", statErr)
	}
	assertLogLines(t, harness.hostActionLog, nil)
}

func TestNativeServerInstallerRejectsMissingInputsAndNonRoot(t *testing.T) {
	requireInstallerTestRoot(t)
	harness := newServerInstallHarness(t)

	tests := []struct {
		name        string
		binary      string
		webBundle   string
		environment string
	}{
		{name: "binary", binary: filepath.Join(harness.base, "missing-server"), webBundle: harness.webBundle, environment: harness.environment},
		{name: "web bundle", binary: harness.binary, webBundle: filepath.Join(harness.base, "missing-web"), environment: harness.environment},
		{name: "environment", binary: harness.binary, webBundle: harness.webBundle, environment: filepath.Join(harness.base, "missing.env")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := harness.command(test.binary, test.webBundle, test.environment, "")
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("installer accepted missing %s", test.name)
			}
			if !strings.Contains(string(output), "missing") {
				t.Fatalf("unexpected missing-input error: %s", output)
			}
			harness.assertNoSecrets(t, string(output))
		})
	}

	command := nonRootInstallerCommand(t, harness.binary, harness.webBundle, harness.environment)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "run as root") {
		t.Fatalf("installer did not reject a non-root production install: %v\n%s", err, output)
	}
}

func nonRootInstallerCommand(t *testing.T, binary, webBundle, environment string) *exec.Cmd {
	t.Helper()
	if os.Geteuid() != 0 {
		return exec.Command("./install-server.sh", binary, webBundle, environment)
	}
	dir, err := os.MkdirTemp("/tmp", "flowlens-install-nonroot-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "install-server.sh")
	writeExecutable(t, script, readScript(t, "install-server.sh"))
	if err := os.Chmod(script, 0755); err != nil {
		t.Fatal(err)
	}
	return exec.Command("/usr/sbin/runuser", "-u", "nobody", "--", "/bin/sh", script, binary, webBundle, environment)
}

type serverInstallHarness struct {
	base           string
	root           string
	binary         string
	webBundle      string
	environment    string
	hostActionLog  string
	databaseURL    string
	agentToken     string
	bootstrapToken string
	secretKey      string
}

func newServerInstallHarness(t *testing.T) *serverInstallHarness {
	t.Helper()
	base := t.TempDir()
	harness := &serverInstallHarness{
		base:           base,
		root:           filepath.Join(base, "root"),
		binary:         filepath.Join(base, "custom-server"),
		webBundle:      filepath.Join(base, "custom-web"),
		environment:    filepath.Join(base, "custom.env"),
		hostActionLog:  filepath.Join(base, "host-actions.log"),
		databaseURL:    "postgres://flowlens:database-secret@127.0.0.1:5432/flowlens?sslmode=disable",
		agentToken:     "agent-secret",
		bootstrapToken: "bootstrap-secret",
		secretKey:      strings.Repeat("a", 64),
	}
	for _, dir := range []string{harness.root, harness.webBundle} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable(t, harness.binary, "#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(filepath.Join(harness.webBundle, "index.html"), []byte("first bundle"), 0644); err != nil {
		t.Fatal(err)
	}
	environment := strings.Join([]string{
		"FLOWLENS_DATABASE_URL=" + harness.databaseURL,
		"FLOWLENS_AGENT_TOKEN=" + harness.agentToken,
		"FLOWLENS_BOOTSTRAP_TOKEN=" + harness.bootstrapToken,
		"FLOWLENS_SECRET_KEY=" + harness.secretKey,
		"FLOWLENS_PUBLIC_URL=https://monitor.example.com",
		"",
	}, "\n")
	if err := os.WriteFile(harness.environment, []byte(environment), 0600); err != nil {
		t.Fatal(err)
	}
	truncateFile(t, harness.hostActionLog)
	return harness
}

func (h *serverInstallHarness) command(binary, webBundle, environment, start string) *exec.Cmd {
	command := exec.Command("./install-server.sh", binary, webBundle, environment)
	command.Env = append(os.Environ(),
		"FLOWLENS_INSTALL_ROOT="+h.root,
		"FLOWLENS_INSTALL_TEST_HOST_ACTION_LOG="+h.hostActionLog,
	)
	if start != "" {
		command.Env = append(command.Env, "FLOWLENS_INSTALL_START="+start)
	}
	return command
}

func (h *serverInstallHarness) run(start string) (string, error) {
	output, err := h.command(h.binary, h.webBundle, h.environment, start).CombinedOutput()
	return string(output), err
}

func (h *serverInstallHarness) assertNoSecrets(t *testing.T, output string) {
	t.Helper()
	for _, secret := range []string{h.databaseURL, h.agentToken, h.bootstrapToken, h.secretKey} {
		if strings.Contains(output, secret) {
			t.Fatalf("installer output exposed a configuration value: %s", output)
		}
	}
}

func (h *serverInstallHarness) assertInstalledFiles(t *testing.T, webContents string) {
	t.Helper()
	assertFile(t, filepath.Join(h.root, "usr/local/bin/flowlens-server"), 0755, "#!/bin/sh\nexit 0\n")
	assertFile(t, filepath.Join(h.root, "opt/flowlens/web/index.html"), 0644, webContents)
	assertFile(t, filepath.Join(h.root, "etc/flowlens/server.env"), 0600, readFile(t, h.environment))
	assertFile(t, filepath.Join(h.root, "etc/systemd/system/flowlens-server.service"), 0644, readFile(t, "../deploy/flowlens-server.service"))
	assertDirectory(t, filepath.Join(h.root, "usr/local/bin"), 0755)
	assertDirectory(t, filepath.Join(h.root, "opt/flowlens"), 0755)
	assertDirectory(t, filepath.Join(h.root, "opt/flowlens/web"), 0755)
	assertDirectory(t, filepath.Join(h.root, "etc/flowlens"), 0755)
	assertDirectory(t, filepath.Join(h.root, "etc/systemd/system"), 0755)
	assertDirectory(t, filepath.Join(h.root, "var/lib/flowlens"), 0750)
	assertDirectory(t, filepath.Join(h.root, "var/lib/flowlens/geoip"), 0750)
}

func requireInstallerTestRoot(t *testing.T) {
	t.Helper()
	if !strings.Contains(readScript(t, "install-server.sh"), "FLOWLENS_INSTALL_ROOT") {
		t.Fatal("install-server.sh needs a controlled destination root before executable tests can run safely")
	}
}

func assertFile(t *testing.T, path string, mode os.FileMode, contents string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Errorf("%s mode = %04o, want %04o", path, info.Mode().Perm(), mode)
	}
	assertCurrentOwner(t, path, info)
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != contents {
		t.Errorf("%s contents = %q, want %q", path, actual, contents)
	}
}

func assertDirectory(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != mode {
		t.Errorf("%s mode = %v, want directory %04o", path, info.Mode(), mode)
	}
	assertCurrentOwner(t, path, info)
}

func assertCurrentOwner(t *testing.T, path string, info os.FileInfo) {
	t.Helper()
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s has unsupported stat data", path)
	}
	if int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() {
		t.Errorf("%s owner = %d:%d, want %d:%d", path, stat.Uid, stat.Gid, os.Geteuid(), os.Getegid())
	}
}

func assertLogLines(t *testing.T, path string, want []string) {
	t.Helper()
	contents := strings.TrimSpace(readFile(t, path))
	var got []string
	if contents != "" {
		got = strings.Split(contents, "\n")
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s lines = %q, want %q", path, got, want)
	}
}

func truncateFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatal(err)
	}
}

func readScript(t *testing.T, name string) string {
	t.Helper()
	return readFile(t, filepath.Join(name))
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
