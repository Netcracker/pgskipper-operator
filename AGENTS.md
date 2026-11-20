# AGENTS.md

Instructions for AI coding assistants working in `pgskipper-operator`.
Use repository source code and Helm templates as the source of truth.

## Repository Layout

- `operator/` — Go operator, API types, controllers, reconcilers, and Helm charts.
- `operator/api/patroni/v1/` — `PatroniCore` API types.
- `operator/api/apps/v1/` — `PatroniServices` API types.
- `operator/controllers/` — Custom-resource controllers.
- `operator/pkg/` — Reconciliation, deployment, storage, and service logic.
- `operator/charts/patroni-core/` — PostgreSQL and Patroni Helm chart.
- `operator/charts/patroni-services/` — Supporting-services Helm chart.
- `services/` — Backup daemon, DBaaS adapter, monitoring, Patroni,
  pgBackRest sidecar, query exporter, replication controller, and upgrade.
- `tests/robot/` — Robot Framework integration suites.
- `docs/` — Installation, configuration, features, and troubleshooting.

## Operator Modes

`operator/cmd/pgskipper-operator/main.go` selects the controller by `OPERATOR_ROLE`:

- `patroni`: reconcile `PatroniCore` and the PostgreSQL/Patroni cluster.
- Other values: reconcile `PatroniServices` and supporting components.

Check the appropriate CRD, controller, Helm template, and reconciler before
modifying behavior. Core and Services are separate Helm releases.

## Backward Compatibility

- Changes to `PatroniCore` and `PatroniServices` CRDs must be additive:
  introduce only optional fields and preserve existing field semantics.
- Do not remove, rename, or change the type of an existing CRD field.
- Do not remove or rename existing Helm values, or change their meaning.
- New Helm values must have defaults that preserve existing deployments.
- If a breaking change appears necessary, **stop and ask for explicit human
  approval** before editing APIs, chart behavior, or migration logic.
- Preserve upgrade and rollback compatibility; never assume a new image or
  version is safe without checking the corresponding upgrade implementation.

## Safe Operations

- Do not run `make local`, `make all`, `make docker-push`, or any target that
  invokes `docker push` unless the user explicitly requests image publishing.
  In `operator/Makefile`, `local` and `all` depend on `docker-push`.
- Do not delete PVCs, clear PostgreSQL data directories, force failover, or
  run destructive restore/rollback steps without explicit approval.
- Never use production clusters for integration tests; the Robot installation
  suite writes test records to PostgreSQL.
- For documentation-only changes, do not modify commands, YAML values, or
  operational procedures unless the user explicitly requests a technical fix.

## Verification (Repository Root)

The root `Makefile` provides read-only formatting checks and verification:

```bash
make modules  # List Go modules
make verify   # Go fmt, vet, golangci-lint, Go tests, Python flake8
```

- After changing Go or Python source, run `make verify` from repository root.
  Pass: command exits `0`; report any failed step and its output.
- After a change under `operator/`, additionally run the operator tests:

```bash
cd operator
go test ./...
```

Pass: exit code `0`, with no failed Go tests. Return to the repository root
before running root-level Makefile targets.
Use `.github/tool-versions.env` for pinned local tool versions.
CI runs `make verify` in `.github/workflows/pre-check.yaml`.
If the root Makefile is missing on the current branch, do not claim that
`make verify` ran; report the missing prerequisite.

## Robot Integration Tests

After any change under `operator/`, run a relevant Robot suite against an
**isolated, deployed test cluster** with required Python libraries and
Kubernetes credentials available. Example (from repository root):

```bash
POD_NAMESPACE=postgres PGSSLMODE=prefer INTERNAL_TLS_ENABLED=false \
  robot --outputdir /tmp/pgskipper-robot \
  --include check_installation_patroni \
  tests/robot/check_installation/check_installation.robot
```

Pass: Robot exits `0` and the selected test passes. Set the namespace, TLS
options, credentials, and cluster connection for the actual test environment.
If a cluster or dependencies are unavailable, **report the test as blocked**;
do not claim it passed. CI integration runs via `.github/workflows/run_tests.yaml`.

## API and Helm Changes

- After changing API types in `operator/api/`, regenerate the CRDs and
  generated Go artifacts, then inspect the diff:

```bash
cd operator
make generate
git diff -- api/ charts/patroni-core/crds/ charts/patroni-services/crds/
```

Pass: generation succeeds, generated changes are expected, and `make verify`
passes from the root. There is no `make manifests` target in this repository.
- After editing either Helm chart, render the affected chart with its sample
  values and inspect the output; do not deploy to a cluster as a lint check:

```bash
helm template patroni-core operator/charts/patroni-core \
  -f operator/charts/patroni-core/patroni-core-quickstart-sample.yaml >/dev/null
helm template patroni-services operator/charts/patroni-services \
  -f operator/charts/patroni-services/patroni-services-quickstart-sample.yaml >/dev/null
```

Pass: affected `helm template` command exits `0`; inspect rendered changes for
correct Kubernetes resource names, defaults, and backward compatibility.

## Documentation

- [Documentation index](docs/README.md) — navigation and feature guides.
- [Quickstart](docs/public/quickstart.md) — deployment steps; do not duplicate.
- [Installation](docs/public/installation.md) — Helm values and prerequisites.
- [Troubleshooting](docs/public/troubleshooting.md) — operational diagnostics.
- When adding, moving, or renaming a guide, update `docs/README.md` and all
  referencing relative links. Pass: each local link resolves to an existing
  file and relevant heading. Do not invent links to documents not yet added.

## Commits

- Never create a commit until the user provides a ticket number.
- Use `<type>: [<ticket-number>] <meaningful message>`.
- Example: `fix: [CPCAP-1234] correct operator validation`.
- Before committing, summarize changed files, validation results, and any
  blocked checks. Do not push or merge without an explicit user request.
