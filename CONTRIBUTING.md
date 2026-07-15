# Contributing to FlowLens

Contributions should be narrowly scoped, tested, and free of private infrastructure data. Use synthetic fixtures and reserved example domains and addresses. Do not commit credentials, environment files, database copies, traffic exports, GeoIP databases, browser artifacts, or local state.

## Local Checks

Use the Go version from `go.mod` and a current Node.js/npm toolchain. Install frontend dependencies with `cd web && npm ci`.

Run the Go suite with `go test ./...` and static analysis with `go vet ./...`.

Run frontend tests with `cd web && npm test` and compile the production bundle with `cd web && npm run build`.

Check every shell script with `sh -n scripts/*.sh` and `shellcheck scripts/*.sh`.

Run the repository privacy scanner with `sh scripts/privacy-check.sh`. Maintainers may additionally point `FLOWLENS_PRIVACY_DENYLIST_FILE` at an untracked `/.flowlens-privacy-denylist` file containing one private literal per line. The scanner is useful without that private denylist because its structural checks always run.

`go test ./...` skips PostgreSQL integration tests unless their database environment is configured. Run the isolated integration suite with PostgreSQL 17 and PostgreSQL client tools (`createdb` and `dropdb`) installed. `FLOWLENS_POSTGRES_ADMIN_URL` must name an administrative connection to a disposable PostgreSQL instance; the script creates and removes a separate database for each package. Never point it at a production or shared instance.

You may use a natively installed PostgreSQL 17 server. For a disposable alternative, start this test-only PostgreSQL 17 container with `docker run --detach --rm --name flowlens-integration-postgres -e POSTGRES_USER=flowlens_ci -e POSTGRES_PASSWORD=flowlens_ci -e POSTGRES_DB=postgres -p 127.0.0.1:55432:5432 postgres:17`. This is not a FlowLens deployment; it is only a local test dependency. Run the suite with `FLOWLENS_POSTGRES_ADMIN_URL='postgres://flowlens_ci:flowlens_ci@127.0.0.1:55432/postgres?sslmode=disable' sh scripts/ci-integration.sh`, then remove the test service with `docker stop flowlens-integration-postgres`.

## Changes

Add or update tests before implementation changes. Keep native deployment as the supported installation model, document security consequences of new privileges or stored data, and avoid claims based on measurements that were not run. By submitting a contribution, you agree that it is licensed under Apache-2.0.
