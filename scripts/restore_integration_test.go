package scripts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRestoreRollsBackFailedArchiveAtomically(t *testing.T) {
	container := os.Getenv("FLOWLENS_RESTORE_INTEGRATION_CONTAINER")
	if container == "" {
		t.Skip("FLOWLENS_RESTORE_INTEGRATION_CONTAINER is not configured")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(container) {
		t.Fatal("integration container name is invalid")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is unavailable")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	sourceDB := "fl_restore_src_" + suffix
	targetDB := "fl_restore_dst_" + suffix
	role := "fl_restore_role_" + suffix
	runDockerSQL(t, container, "postgres", fmt.Sprintf(`CREATE ROLE %s LOGIN`, role))
	t.Cleanup(func() {
		_ = dockerCommand(container, "dropdb", "-U", "postgres", "--if-exists", "--force", sourceDB).Run()
		_ = dockerCommand(container, "dropdb", "-U", "postgres", "--if-exists", "--force", targetDB).Run()
		_ = dockerCommand(container, "psql", "-U", "postgres", "-d", "postgres", "-c", "DROP ROLE IF EXISTS "+role).Run()
	})
	requireDockerCommand(t, container, "createdb", "-U", "postgres", sourceDB)
	requireDockerCommand(t, container, "createdb", "-U", "postgres", "-O", role, targetDB)
	runDockerSQL(t, container, sourceDB, `
CREATE TABLE public.alpha_restore_probe(id integer PRIMARY KEY);
CREATE FUNCTION public.restore_event_probe() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN END $$;
CREATE EVENT TRIGGER restore_event_probe ON ddl_command_start EXECUTE FUNCTION public.restore_event_probe();
ALTER EVENT TRIGGER restore_event_probe DISABLE;
INSERT INTO public.alpha_restore_probe VALUES (1);
`)

	dir := t.TempDir()
	dump := filepath.Join(dir, "flowlens-atomic.dump")
	dumpCommand := dockerCommand(container, "pg_dump", "-U", "postgres", "-Fc", "-d", sourceDB)
	archive, err := dumpCommand.Output()
	if err != nil {
		t.Fatalf("create custom archive: %v", err)
	}
	if err := os.WriteFile(dump, archive, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	checksum := fmt.Sprintf("%x  %s\n", digest, filepath.Base(dump))
	if err := os.WriteFile(filepath.Join(dir, "flowlens-atomic.sha256"), []byte(checksum), 0600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	actionLog := filepath.Join(dir, "actions.log")
	started := filepath.Join(dir, "started")
	truncateFile(t, actionLog)
	truncateFile(t, started)
	writeRestoreIntegrationCommands(t, bin, container)
	appURL := "postgres://" + role + ":SENTINEL_ATOMIC_SECRET@unused/" + targetDB
	baseEnv := append(os.Environ(),
		"FLOWLENS_RESTORE_TEST_ROOT="+dir,
		"FLOWLENS_DATABASE_URL="+appURL,
		"FLOWLENS_SYSTEMCTL="+filepath.Join(bin, "systemctl"),
		"FLOWLENS_CURL="+filepath.Join(bin, "curl"),
		"FLOWLENS_SHA256SUM=sha256sum",
		"FLOWLENS_SLEEP="+filepath.Join(bin, "sleep"),
		"FLOWLENS_PSQL="+filepath.Join(bin, "psql"),
		"FLOWLENS_PG_RESTORE="+filepath.Join(bin, "pg_restore"),
		"FLOWLENS_RESTORE_HEALTH_ATTEMPTS=1",
		"FLOWLENS_TEST_ACTION_LOG="+actionLog,
		"FLOWLENS_TEST_STARTED_UNITS="+started,
		"FLOWLENS_TEST_RESTORE_USER="+role,
	)

	command := exec.Command("./restore.sh", dump)
	command.Env = baseEnv
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("limited restore unexpectedly succeeded: %s", output)
	}
	if !strings.Contains(strings.ToLower(string(output)), "event trigger") {
		t.Fatalf("restore did not fail at the intentional mid-archive event trigger:\n%s", output)
	}
	if strings.Contains(string(output), "SENTINEL_ATOMIC_SECRET") {
		t.Fatalf("restore exposed database URL: %s", output)
	}
	if strings.Contains(readFile(t, actionLog), "systemctl:start") {
		t.Fatal("restore started services after transactional failure")
	}
	if objects := countRestoreProbeObjects(t, container, targetDB); objects != 0 {
		t.Fatalf("failed restore left %d user objects; restore was not atomic", objects)
	}

	truncateFile(t, actionLog)
	command = exec.Command("./restore.sh", dump)
	command.Env = append(append([]string{}, baseEnv...), "FLOWLENS_TEST_RESTORE_USER=postgres")
	output, err = command.CombinedOutput()
	if err != nil {
		t.Fatalf("retry after rolled-back restore failed: %v\n%s", err, output)
	}
	if rows := dockerScalar(t, container, targetDB, "SELECT count(*) FROM public.alpha_restore_probe"); rows != "1" {
		t.Fatalf("retry restored %s rows, want 1", rows)
	}
}

func writeRestoreIntegrationCommands(t *testing.T, bin, container string) {
	t.Helper()
	writeExecutable(t, filepath.Join(bin, "systemctl"), `#!/bin/sh
case "$1" in
  show) printf 'inactive\n' ;;
  start) printf 'systemctl:start %s\n' "$2" >>"$FLOWLENS_TEST_ACTION_LOG"; printf '%s\n' "$2" >>"$FLOWLENS_TEST_STARTED_UNITS" ;;
  is-active) exit 0 ;;
esac
`)
	writeExecutable(t, filepath.Join(bin, "curl"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bin, "sleep"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bin, "psql"), fmt.Sprintf(`#!/bin/sh
set -eu
database=${PGDATABASE##*/}
database=${database%%%%\?*}
exec docker exec -i %s psql -U "$FLOWLENS_TEST_RESTORE_USER" -d "$database" "$@"
`, container))
	writeExecutable(t, filepath.Join(bin, "pg_restore"), fmt.Sprintf(`#!/bin/sh
set -eu
archive=
direct=false
for argument in "$@"; do
  case "$argument" in
    --dbname=) direct=true ;;
    *.dump) archive=$argument ;;
  esac
done
database=${PGDATABASE##*/}
database=${database%%%%\?*}
if [ "$direct" = true ]; then
  exec docker exec -i -e PGDATABASE="$database" %s pg_restore -U "$FLOWLENS_TEST_RESTORE_USER" --dbname='' --single-transaction --exit-on-error --no-owner --no-privileges <"$archive"
fi
exec docker exec -i %s pg_restore -U "$FLOWLENS_TEST_RESTORE_USER" --file=- --no-owner --no-privileges <"$archive"
`, container, container))
}

func countRestoreProbeObjects(t *testing.T, container, database string) int {
	t.Helper()
	query := `SELECT
  (SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN ('r','p','S','v','m')) +
  (SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace) +
  (SELECT count(*) FROM pg_event_trigger)`
	value := dockerScalar(t, container, database, query)
	var count int
	if _, err := fmt.Sscan(value, &count); err != nil {
		t.Fatalf("parse object count %q: %v", value, err)
	}
	return count
}

func dockerScalar(t *testing.T, container, database, query string) string {
	t.Helper()
	command := dockerCommand(container, "psql", "-U", "postgres", "-d", database, "-Atc", query)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("query disposable database: %v\n%s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func runDockerSQL(t *testing.T, container, database, query string) {
	t.Helper()
	requireDockerCommand(t, container, "psql", "-U", "postgres", "-d", database, "-v", "ON_ERROR_STOP=1", "-c", query)
}

func requireDockerCommand(t *testing.T, container string, arguments ...string) {
	t.Helper()
	command := dockerCommand(container, arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("docker postgres command failed: %v\n%s", err, output)
	}
}

func dockerCommand(container string, arguments ...string) *exec.Cmd {
	all := append([]string{"exec", container}, arguments...)
	return exec.Command("docker", all...)
}
