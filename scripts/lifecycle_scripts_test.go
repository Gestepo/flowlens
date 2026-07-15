package scripts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const restoreSentinel = "postgres://flowlens:SENTINEL_DATABASE_SECRET@db.example/flowlens"

type restoreHarness struct {
	dir     string
	bin     string
	log     string
	dump    string
	secret  string
	baseEnv []string
	script  string
	root    string
	started string
}

func newRestoreHarness(t *testing.T) *restoreHarness {
	t.Helper()
	script := readScript(t, "restore.sh")
	for _, hook := range []string{"FLOWLENS_RESTORE_TEST_ROOT", "FLOWLENS_SYSTEMCTL", "FLOWLENS_CURL", "FLOWLENS_SHA256SUM", "FLOWLENS_SLEEP", "FLOWLENS_RESTORE_HEALTH_ATTEMPTS"} {
		if !strings.Contains(script, hook) {
			t.Fatalf("restore.sh needs controlled harness hook %s", hook)
		}
	}
	dir := t.TempDir()
	h := &restoreHarness{
		dir:     dir,
		bin:     filepath.Join(dir, "bin"),
		log:     filepath.Join(dir, "actions.log"),
		dump:    filepath.Join(dir, "flowlens-test.dump"),
		secret:  restoreSentinel,
		script:  "./restore.sh",
		root:    filepath.Join(dir, "root"),
		started: filepath.Join(dir, "started-units"),
	}
	for _, path := range []string{h.bin, h.root} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(h.dump, []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flowlens-test.sha256"), []byte("checksum fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	truncateFile(t, h.log)
	truncateFile(t, h.started)
	h.writeCommands(t)
	h.baseEnv = append(os.Environ(),
		"FLOWLENS_RESTORE_TEST_ROOT="+h.root,
		"FLOWLENS_DATABASE_URL="+h.secret,
		"FLOWLENS_SYSTEMCTL="+filepath.Join(h.bin, "systemctl"),
		"FLOWLENS_CURL="+filepath.Join(h.bin, "curl"),
		"FLOWLENS_SHA256SUM="+filepath.Join(h.bin, "sha256sum"),
		"FLOWLENS_SLEEP="+filepath.Join(h.bin, "sleep"),
		"FLOWLENS_PSQL="+filepath.Join(h.bin, "psql"),
		"FLOWLENS_PG_RESTORE="+filepath.Join(h.bin, "pg_restore"),
		"FLOWLENS_RESTORE_HEALTH_ATTEMPTS=2",
		"FLOWLENS_TEST_ACTION_LOG="+h.log,
		"FLOWLENS_TEST_STARTED_UNITS="+h.started,
		"FLOWLENS_TEST_EXPECTED_DATABASE_URL="+h.secret,
		"FLOWLENS_TEST_TABLES=0",
		"FLOWLENS_TEST_ACTIVE_UNITS=",
		"FLOWLENS_TEST_CURL_STATUS=0",
	)
	return h
}

func (h *restoreHarness) writeCommands(t *testing.T) {
	t.Helper()
	writeExecutable(t, filepath.Join(h.bin, "systemctl"), `#!/bin/sh
set -eu
printf 'systemctl:%s\n' "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
case "$1" in
  show)
    unit=${2:-}
    [ "${FLOWLENS_TEST_STATE_QUERY_FAILURE:-}" != "$unit" ] || exit 1
    case ",${FLOWLENS_TEST_ACTIVE_UNITS:-}," in *,"$unit",*) printf 'active\n'; exit 0;; esac
    case "$unit" in
      flowlens-server.service) printf '%s\n' "${FLOWLENS_TEST_SERVER_STATE-inactive}" ;;
      flowlens-agent.service) printf '%s\n' "${FLOWLENS_TEST_AGENT_STATE-inactive}" ;;
      *) printf 'unknown\n' ;;
    esac
    ;;
  is-active)
    unit=${3:-}
    case ",${FLOWLENS_TEST_ACTIVE_UNITS:-}," in *,"$unit",*) exit 0;; esac
    grep -Fx "$unit" "$FLOWLENS_TEST_STARTED_UNITS" >/dev/null 2>&1 && exit 0
    exit 3
    ;;
  start)
    [ "${FLOWLENS_TEST_FAIL_START:-}" != "$2" ] || exit 1
    printf '%s\n' "$2" >>"$FLOWLENS_TEST_STARTED_UNITS"
    ;;
esac
`)
	writeExecutable(t, filepath.Join(h.bin, "sha256sum"), `#!/bin/sh
set -eu
printf 'sha256sum:%s\n' "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
exit "${FLOWLENS_TEST_CHECKSUM_STATUS:-0}"
`)
	writeExecutable(t, filepath.Join(h.bin, "psql"), `#!/bin/sh
set -eu
printf 'psql:%s\n' "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
[ "${PGDATABASE:-}" = "$FLOWLENS_TEST_EXPECTED_DATABASE_URL" ] || exit 97
case "$*" in
  *"SELECT count(*)"*)
    [ "${FLOWLENS_TEST_EMPTY_QUERY_STATUS:-0}" -eq 0 ] || exit "$FLOWLENS_TEST_EMPTY_QUERY_STATUS"
    printf '%s\n' "${FLOWLENS_TEST_TABLES:-0}"
    ;;
  *)
    cat >/dev/null
    exit "${FLOWLENS_TEST_RESTORE_PSQL_STATUS:-0}"
    ;;
esac
`)
	writeExecutable(t, filepath.Join(h.bin, "pg_restore"), `#!/bin/sh
set -eu
printf 'pg_restore:%s\n' "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
[ "${PGDATABASE:-}" = "$FLOWLENS_TEST_EXPECTED_DATABASE_URL" ] || exit 97
[ "${FLOWLENS_TEST_PG_RESTORE_STATUS:-0}" -eq 0 ] || exit "$FLOWLENS_TEST_PG_RESTORE_STATUS"
`)
	writeExecutable(t, filepath.Join(h.bin, "curl"), `#!/bin/sh
set -eu
printf 'curl:%s\n' "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
exit "${FLOWLENS_TEST_CURL_STATUS:-0}"
`)
	writeExecutable(t, filepath.Join(h.bin, "sleep"), `#!/bin/sh
set -eu
printf 'sleep:%s\n' "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
`)
}

func (h *restoreHarness) run(extra ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, h.script, h.dump)
	command.Env = append(append([]string{}, h.baseEnv...), extra...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestRestoreRejectsUnusableServiceStates(t *testing.T) {
	tests := []struct {
		name string
		env  []string
	}{
		{name: "query failure", env: []string{"FLOWLENS_TEST_STATE_QUERY_FAILURE=flowlens-server.service"}},
		{name: "empty", env: []string{"FLOWLENS_TEST_SERVER_STATE="}},
		{name: "activating", env: []string{"FLOWLENS_TEST_AGENT_STATE=activating"}},
		{name: "unknown", env: []string{"FLOWLENS_TEST_SERVER_STATE=mystery"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newRestoreHarness(t)
			output, err := h.run(test.env...)
			if err == nil {
				t.Fatalf("restore accepted unusable service state: %s", output)
			}
			actions := h.actions(t)
			if strings.Contains(actions, "psql:") || strings.Contains(actions, "pg_restore:") {
				t.Fatalf("restore reached database work after service-state failure:\n%s", actions)
			}
		})
	}
}

func (h *restoreHarness) actions(t *testing.T) string {
	t.Helper()
	return readFile(t, h.log)
}

func TestRestoreRefusesEitherActiveService(t *testing.T) {
	for _, unit := range []string{"flowlens-server.service", "flowlens-agent.service", "flowlens-server.service,flowlens-agent.service"} {
		t.Run(unit, func(t *testing.T) {
			h := newRestoreHarness(t)
			output, err := h.run("FLOWLENS_TEST_ACTIVE_UNITS=" + unit)
			expected := strings.Split(unit, ",")[0]
			if err == nil || !strings.Contains(output, "stop "+expected) {
				t.Fatalf("restore did not refuse active %s: %v\n%s", unit, err, output)
			}
			if strings.Contains(h.actions(t), "pg_restore:") {
				t.Fatal("restore ran before service-state validation completed")
			}
		})
	}
}

func TestRestoreRequiresEmptyDatabaseAndStartsInOrder(t *testing.T) {
	t.Run("nonempty", func(t *testing.T) {
		h := newRestoreHarness(t)
		output, err := h.run("FLOWLENS_TEST_TABLES=1")
		if err == nil || !strings.Contains(output, "target database must be empty") {
			t.Fatalf("restore accepted nonempty database: %v\n%s", err, output)
		}
		if strings.Contains(h.actions(t), "pg_restore:") || strings.Contains(h.actions(t), "systemctl:start") {
			t.Fatal("restore proceeded after nonempty database check")
		}
	})

	t.Run("success", func(t *testing.T) {
		h := newRestoreHarness(t)
		output, err := h.run()
		if err != nil {
			t.Fatalf("restore failed: %v\n%s\n%s", err, output, h.actions(t))
		}
		actions := h.actions(t)
		if !strings.Contains(actions, "pg_restore:--dbname= --single-transaction --exit-on-error") {
			t.Fatalf("restore did not request an atomic direct database restore:\n%s", actions)
		}
		if strings.Count(actions, "psql:") != 1 {
			t.Fatalf("restore must use psql only for the empty-database check:\n%s", actions)
		}
		assertOrdered(t, actions,
			"pg_restore:",
			"systemctl:start flowlens-server.service",
			"curl:",
			"systemctl:start flowlens-agent.service",
		)
		for _, exposed := range []string{h.secret, "SENTINEL_DATABASE_SECRET"} {
			if strings.Contains(output, exposed) || strings.Contains(actions, exposed) {
				t.Fatalf("restore exposed database secret %q\noutput=%s\nactions=%s", exposed, output, actions)
			}
		}
	})
}

func TestAcceptanceNativeAdapterRequiresDistinctAdminURL(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "postgres-actions.log")
	password := filepath.Join(dir, "admin-password")
	if err := os.WriteFile(password, []byte("unused"), 0600); err != nil {
		t.Fatal(err)
	}
	truncateFile(t, log)
	createdb := filepath.Join(dir, "createdb")
	dropdb := filepath.Join(dir, "dropdb")
	psql := filepath.Join(dir, "psql")
	writeExecutable(t, createdb, `#!/bin/sh
printf 'createdb:%s:%s:%s:%s:password=%s:args=%s\n' "${PGHOST:-}" "${PGPORT:-}" "${PGUSER:-}" "${PGDATABASE:-}" "${PGPASSWORD:+set}" "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
exit 42
`)
	writeExecutable(t, dropdb, `#!/bin/sh
printf 'dropdb:%s:%s:%s:%s:password=%s:args=%s\n' "${PGHOST:-}" "${PGPORT:-}" "${PGUSER:-}" "${PGDATABASE:-}" "${PGPASSWORD:+set}" "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
`)
	writeExecutable(t, psql, "#!/bin/sh\nexit 1\n")
	base := environmentWithout(os.Environ(), "FLOWLENS_POSTGRES_ADMIN_URL")
	base = append(base,
		"FLOWLENS_DATABASE_URL=postgres://app:APP_SECRET@app.example/flowlens",
		"FLOWLENS_POSTGRES_ADAPTER=native",
		"FLOWLENS_CREATEDB="+createdb,
		"FLOWLENS_DROPDB="+dropdb,
		"FLOWLENS_PSQL="+psql,
		"FLOWLENS_ADMIN_PASSWORD_FILE="+password,
		"FLOWLENS_CAPTURE_INTERFACE=lo",
		"FLOWLENS_TEST_ACTION_LOG="+log,
	)

	command := exec.Command("./acceptance.sh")
	command.Env = base
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "FLOWLENS_POSTGRES_ADMIN_URL is required") {
		t.Fatalf("acceptance did not reject missing native admin URL: %v\n%s", err, output)
	}
	if contents := readFile(t, log); contents != "" {
		t.Fatalf("acceptance invoked database commands without an admin URL:\n%s", contents)
	}

	command = exec.Command("./acceptance.sh")
	command.Env = append(append([]string{}, base...), "FLOWLENS_POSTGRES_ADMIN_URL=postgres://app:APP_SECRET@app.example/flowlens")
	output, err = command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "must differ") {
		t.Fatalf("acceptance accepted the application URL as its admin URL: %v\n%s", err, output)
	}

	truncateFile(t, log)
	command = exec.Command("./acceptance.sh")
	command.Env = append(append([]string{}, base...), "FLOWLENS_POSTGRES_ADMIN_URL=postgres://admin:ADMIN_SECRET@admin.example/postgres")
	output, err = command.CombinedOutput()
	if err == nil {
		t.Fatalf("createdb sentinel was expected to stop acceptance: %s", output)
	}
	actions := readFile(t, log)
	if strings.Contains(actions, "APP_SECRET") || strings.Contains(actions, "app.example") {
		t.Fatalf("acceptance used the application URL for database administration:\n%s", actions)
	}
	for _, operation := range []string{
		"createdb:admin.example:5432:admin:postgres:password=set:args=--owner=flowlens",
		"dropdb:admin.example:5432:admin:postgres:password=set:args=--if-exists --force",
	} {
		if !strings.Contains(actions, operation) {
			t.Fatalf("acceptance did not use the admin URL for %s:\n%s", operation, actions)
		}
	}
	if strings.Contains(actions, "ADMIN_SECRET") || strings.Contains(actions, "postgres://") {
		t.Fatalf("acceptance exposed the admin URL through command arguments:\n%s", actions)
	}
	if strings.Contains(string(output), "APP_SECRET") || strings.Contains(string(output), "ADMIN_SECRET") {
		t.Fatalf("acceptance exposed a database URL: %s", output)
	}
}

func TestAcceptanceNativeQueriesUseParsedLibpqEnvironment(t *testing.T) {
	contents := readFile(t, "acceptance.sh")
	for _, fragment := range []string{
		"postgres_database_host=",
		"postgres_database_port=",
		"postgres_database_user=",
		"postgres_database_password=",
		"postgres_database_name=",
		"PGHOST=$postgres_database_host PGPORT=$postgres_database_port PGUSER=$postgres_database_user",
		"PGPASSWORD=$postgres_database_password PGDATABASE=$postgres_database_name PGSSLMODE=$postgres_database_sslmode",
	} {
		if !strings.Contains(contents, fragment) {
			t.Fatalf("acceptance.sh must parse the application URL into libpq environment fields: missing %q", fragment)
		}
	}
	if strings.Contains(contents, "native) PGDATABASE=$database_url") {
		t.Fatal("acceptance.sh must not pass the application URL through PGDATABASE")
	}
}

func environmentWithout(values []string, name string) []string {
	prefix := name + "="
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !strings.HasPrefix(value, prefix) {
			result = append(result, value)
		}
	}
	return result
}

func TestRestoreStopsAfterCommandFailures(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		forbidden string
	}{
		{name: "empty query", env: "FLOWLENS_TEST_EMPTY_QUERY_STATUS=1", forbidden: "pg_restore:"},
		{name: "archive decode", env: "FLOWLENS_TEST_PG_RESTORE_STATUS=1", forbidden: "systemctl:start"},
		{name: "server start", env: "FLOWLENS_TEST_FAIL_START=flowlens-server.service", forbidden: "curl:"},
		{name: "health", env: "FLOWLENS_TEST_CURL_STATUS=1", forbidden: "systemctl:start flowlens-agent.service"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newRestoreHarness(t)
			output, err := h.run(test.env)
			if err == nil {
				t.Fatalf("restore ignored %s failure: %s", test.name, output)
			}
			if strings.Contains(h.actions(t), test.forbidden) {
				t.Fatalf("restore continued after %s failure:\n%s", test.name, h.actions(t))
			}
		})
	}
	t.Run("agent start", func(t *testing.T) {
		h := newRestoreHarness(t)
		output, err := h.run("FLOWLENS_TEST_FAIL_START=flowlens-agent.service")
		if err == nil {
			t.Fatalf("restore ignored agent start failure: %s", output)
		}
		actions := h.actions(t)
		if strings.Contains(actions, "systemctl:is-active") {
			t.Fatalf("restore continued to final checks after Agent start failure:\n%s", actions)
		}
	})
}

type uninstallHarness struct {
	root   string
	bin    string
	log    string
	states string
	env    []string
}

func newUninstallHarness(t *testing.T, loadedUnits ...string) *uninstallHarness {
	t.Helper()
	script := readScript(t, "uninstall.sh")
	for _, hook := range []string{"FLOWLENS_UNINSTALL_ROOT", "FLOWLENS_SYSTEMCTL"} {
		if !strings.Contains(script, hook) {
			t.Fatalf("uninstall.sh needs controlled harness hook %s", hook)
		}
	}
	dir := t.TempDir()
	h := &uninstallHarness{root: filepath.Join(dir, "root"), bin: filepath.Join(dir, "bin"), log: filepath.Join(dir, "actions.log"), states: filepath.Join(dir, "units")}
	for _, path := range []string{h.root, h.bin, h.states} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	truncateFile(t, h.log)
	writeExecutable(t, filepath.Join(h.bin, "systemctl"), `#!/bin/sh
set -eu
printf 'systemctl:%s\n' "$*" >>"$FLOWLENS_TEST_ACTION_LOG"
case "$1" in
  show)
    unit=$2
    if [ -f "$FLOWLENS_TEST_UNIT_STATE/$unit" ]; then printf 'loaded\n'; else printf 'not-found\n'; fi
    ;;
  disable)
    unit=$3
    if [ "${FLOWLENS_TEST_FAIL_DISABLE:-}" = "$unit" ]; then exit 1; fi
    rm -f "$FLOWLENS_TEST_UNIT_STATE/$unit"
    ;;
  daemon-reload) ;;
esac
`)
	for _, unit := range loadedUnits {
		if err := os.WriteFile(filepath.Join(h.states, unit), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	h.seedFiles(t)
	h.env = append(os.Environ(),
		"FLOWLENS_UNINSTALL_ROOT="+h.root,
		"FLOWLENS_SYSTEMCTL="+filepath.Join(h.bin, "systemctl"),
		"FLOWLENS_TEST_ACTION_LOG="+h.log,
		"FLOWLENS_TEST_UNIT_STATE="+h.states,
	)
	return h
}

func (h *uninstallHarness) seedFiles(t *testing.T) {
	t.Helper()
	files := []string{
		"etc/systemd/system/flowlens-agent.service", "etc/systemd/system/flowlens-server.service",
		"usr/local/bin/flowlens-agent", "usr/local/bin/flowlens-server", "etc/sysctl.d/60-flowlens-perf.conf",
		"etc/flowlens/server.env", "etc/flowlens/agent.env", "opt/flowlens/web/index.html",
		"var/lib/flowlens/database-marker", "var/lib/flowlens-agent/state-marker", "backups/backup-marker",
	}
	for _, relative := range files {
		path := filepath.Join(h.root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *uninstallHarness) run(extra ...string) (string, error) {
	command := exec.Command("./uninstall.sh")
	command.Env = append(append([]string{}, h.env...), extra...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestUninstallHandlesPartialAndMissingUnitsAndRepeats(t *testing.T) {
	for _, loaded := range [][]string{nil, {"flowlens-server.service"}, {"flowlens-agent.service"}} {
		name := strings.Join(loaded, ",")
		if name == "" {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			h := newUninstallHarness(t, loaded...)
			for run := 1; run <= 2; run++ {
				output, err := h.run()
				if err != nil {
					t.Fatalf("uninstall run %d failed: %v\n%s", run, err, output)
				}
			}
			h.assertRemovedAndPreserved(t)
		})
	}
}

func TestUninstallContinuesCleanupAfterUnexpectedUnitFailure(t *testing.T) {
	h := newUninstallHarness(t, "flowlens-server.service", "flowlens-agent.service")
	output, err := h.run("FLOWLENS_TEST_FAIL_DISABLE=flowlens-agent.service")
	if err == nil {
		t.Fatalf("uninstall hid unexpected systemctl failure: %s", output)
	}
	actions := readFile(t, h.log)
	if !strings.Contains(actions, "disable --now flowlens-server.service") {
		t.Fatalf("uninstall did not process the second unit after failure:\n%s", actions)
	}
	h.assertRemovedAndPreserved(t)
}

func (h *uninstallHarness) assertRemovedAndPreserved(t *testing.T) {
	t.Helper()
	for _, relative := range []string{"etc/systemd/system/flowlens-agent.service", "etc/systemd/system/flowlens-server.service", "usr/local/bin/flowlens-agent", "usr/local/bin/flowlens-server", "etc/flowlens", "opt/flowlens/web"} {
		if _, err := os.Stat(filepath.Join(h.root, relative)); !os.IsNotExist(err) {
			t.Errorf("artifact was not removed: %s (%v)", relative, err)
		}
	}
	for _, relative := range []string{"var/lib/flowlens/database-marker", "var/lib/flowlens-agent/state-marker", "backups/backup-marker"} {
		if _, err := os.Stat(filepath.Join(h.root, relative)); err != nil {
			t.Errorf("preserved data is missing: %s (%v)", relative, err)
		}
	}
}

func assertOrdered(t *testing.T, contents string, fragments ...string) {
	t.Helper()
	position := 0
	for _, fragment := range fragments {
		next := strings.Index(contents[position:], fragment)
		if next < 0 {
			t.Fatalf("%q does not appear after offset %d in:\n%s", fragment, position, contents)
		}
		position += next + len(fragment)
	}
}
