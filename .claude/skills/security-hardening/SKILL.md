---
name: security-hardening
description: Enforce Kubernetes container security hardening across Dockerfiles, Helm charts, and controllers.
---

Apply Kubernetes container security hardening. Make only necessary, minimal, backward-compatible changes.

---

## Required

### Pod securityContext

```yaml
runAsNonRoot: true
seccompProfile:
  type: RuntimeDefault
{{- if eq (default "" .Values.PAAS_PLATFORM) "KUBERNETES" }}
  runAsUser: 1000
  runAsGroup: 1000
{{- end }}
```

Use `default ""` before comparison to avoid nil-type comparison errors when `PAAS_PLATFORM` is not set.

Always check if application already uses `runAsUser` and `runAsGroup` templates and use the same approach.

### Container securityContext

```yaml
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: [ALL]
```

### Forbidden

- hostNetwork / hostPID / hostIPC
- hostPath volumes

### Dockerfile

- Final stage must include:

```dockerfile
USER <uid >= 1000>
```

### Forbidden ports

17–995, 1080, 1236, 1433–1434, 1494, 1512, 1524–1525,  
1645–1646, 1649, 1758–1759, 1789, 1812, 1911, 26000

---

## Rules

- Two tracks:
  - Helm → fix templates
  - Operator → fix CR or controller code

- Operator:
  - CR pass-through → fix Helm (CR spec)
  - Controller-built → fix controller

- Init containers: do not modify existing ones; do not add new ones to fix writable paths.
- Do not change PVCs

### Writable paths

- Default assumption: no writable path changes needed. Only add mounts if the container is known to fail with `readOnlyRootFilesystem: true`.
- For well-known images (opensearch, telegraf, nginx, etc.) look up their known writable paths — do not infer from entrypoint scripts, since static analysis cannot distinguish image-baked writable dirs from runtime writes.
- Never add mounts for log directories (for example, `/logs` or `*/logs`) by default: workloads must write logs to stdout. If such a mount already exists or looks required, add a plan question asking whether it should be removed or replaced. Exception: some components, such as Telegraf-based monitoring agents, may need a mounted output directory for execution scripts, in these cases, ask whether the application output should be changed instead of mounting a log folder.
- If writable paths are uncertain, flag them in the plan for runtime validation rather than omitting `readOnlyRootFilesystem`.
- Never omit `readOnlyRootFilesystem: true` to avoid the problem — add emptyDir mounts for confirmed write paths instead.
- Use:
  - `emptyDir` (dir)
  - `emptyDir + subPath` (file)
- Always set `sizeLimit` that must be calculated base on usage in application scenarios.

### Prefer redirect

- Env-controlled paths → `/tmp`
- Python → `PYTHONDONTWRITEBYTECODE=1`

### Refactor if needed

If modifying image files:

- copy → `/tmp`
- modify there
- use new path

### `/tmp` emptyDir mount — mandatory for every pod

Mount a emptyDir at `/tmp` on **every** pod (operator-built and Helm-managed).
JVM, Python, bash, and many other runtimes silently write temp files there.
The `sizeLimit` must be calculated base on usage in source code of corresponding services. But not more than `100Mi`, use this value also if you cannot calculate, but in that case add the question regarding size to the plan.

```yaml
# volumes:
- name: tmp
  emptyDir:
    sizeLimit: 100Mi
# volumeMounts:
- name: tmp
  mountPath: /tmp
```

### Python containers

Set `PYTHONDONTWRITEBYTECODE=1` on every Python container to prevent `.pyc` write failures:

```yaml
env:
  - name: PYTHONDONTWRITEBYTECODE
    value: "1"
```

---

## Image Refactoring — `readOnlyRootFilesystem: true`

When an entrypoint script mutates files under the image root FS at runtime (writes configs,
generates keystores, patches properties), use the **config-template pattern**:

### Pattern

1. **Dockerfile**: rename baked static config dir from `config/` → `config-template/`.
2. **Entrypoint**: at startup, define a writable work dir under `/tmp/<service>/` and copy
   the template into it:
   ```bash
   SERVICE_CONFIG=/tmp/<service>/config
   mkdir -p "${SERVICE_CONFIG}"
   cp -r "${APP_HOME}/config-template/." "${SERVICE_CONFIG}/"
   ```
3. Replace **all** runtime writes from `${APP_HOME}/config/` to `${SERVICE_CONFIG}/`.
4. Redirect TLS keystore dirs (e.g. `tls-ks/`) to `/tmp/<service>/tls-ks/`.
5. Redirect other generated artefacts (properties files, JAAS conf, lock files, heap dump paths) similarly.
6. Update any env vars that point to config paths via `export` at the top of the entrypoint.
7. For ConfigMap mounts that the entrypoint reads from the old config dir, update the
   operator VolumeMount `MountPath` to the new `/tmp/<service>/config/<subdir>` path.

### Common runtime write categories

- **App config files patched at startup** (`server.properties`, `application.yml`, `log4j.properties`) —
  `config-template` pattern → write to `/tmp/<svc>/config/`
- **TLS keystores / truststores** (`*.jks`, `*.p12`, `cacerts`) —
  redirect generation to `/tmp/<svc>/tls-ks/`
- **CLI credential / client config files** (`client.properties`, `credentials.yml`, `tool.conf`) —
  redirect write; add Dockerfile symlink from original path (see Pitfall 7)
- **Lock / PID / sentinel files** (`.lock`, `.started`, `.pid`) —
  redirect to `/tmp/<svc>/`
- **Heap / core dump paths** (configured via JVM `-XX:HeapDumpPath`) —
  point to `/tmp/<svc>/dumps/`
- **Optional UI or asset bundle copied at startup** (static web assets written or deleted per env-flag) —
  conditionally **copy** to `/tmp/<svc>/ui/` instead of deleting from root FS (see Pitfall 6)

### Checklist before marking an image as readOnly-ready

- [ ] No `sed -i`, `>>`-appends, `cp`, or `mkdir` target any path outside `/tmp` or existing volumes.
- [ ] Helm smoke test: `grep -c 'readOnlyRootFilesystem: true'` returns the expected workload count.
- [ ] Container runs without permission errors in a read-only root FS pod.

---

## Flow

### Step 1 — Cheap audit (grep only, no full file reads)

**Run all greps in parallel in a single message** — they are fully independent.

First, find all chart roots — a repo may have multiple independent charts. `Chart.yaml` is mandatory in every Helm chart and is the reliable marker:

```bash
find . -name "Chart.yaml" 2>/dev/null
```

Each directory containing a `Chart.yaml` is a `<CHART_ROOT>`. Run the full audit below **for each chart root independently**. If a chart root has no `templates/` subdirectory it is a dependency/library chart — skip it.

Then fire all of the following in one parallel batch (repeat per chart root):

**A. Helm — shared helper exists?**

```bash
grep -rn "define.*[Pp]od[Ss]ecurity\|define.*[Cc]ontainer[Ss]ecurity" <CHART_ROOT>/
```

**B. Helm — workloads missing the helper** (output = files that need fixing):

```bash
grep -rL "globalPodSecurityContext" <CHART_ROOT>/templates/ \
  | xargs grep -l "kind: Deployment\|kind: StatefulSet\|kind: Job\|kind: DaemonSet" 2>/dev/null
```

**C. Helm — missing required fields:**

```bash
grep -r "readOnlyRootFilesystem" <CHART_ROOT>/templates/
grep -r "seccompProfile\|runAsNonRoot" <CHART_ROOT>/templates/
```

**D. Helm — forbidden fields:**

```bash
grep -rn "hostNetwork\|hostPID\|hostIPC" <CHART_ROOT>/templates/
grep -rn "hostPath" <CHART_ROOT>/templates/
```

**E. Helm — forbidden ports:**

```bash
grep -rn "containerPort" <CHART_ROOT>/templates/ \
  | grep -E "([^0-9](1[7-9]|[2-9][0-9]|[1-9][0-9]{2}|99[0-5])|1080|1236|143[34]|1494|1512|152[45]|164[56]|1649|175[89]|1789|1812|1911|26000)[^0-9]"
```

**F. Dockerfiles — non-root enforcement:**

```bash
grep -rn "^USER" --include="Dockerfile*" .
```

**G. Entrypoints — runtime writes to root FS:**

```bash
grep -rn "sed -i\|cp .* /\|\bmkdir\b\|\bmv\b\|\s>>\?\s*[/a-zA-Z~\$\.]" --include="*.sh" . \
  | grep -v "/dev/null\|/dev/std\|/tmp"
```

**H. Controllers — programmatic pod spec construction:**

```bash
grep -rn "corev1\.PodSpec{\|PodTemplateSpec{\|SecurityContext{" --include="*.go" .
grep -rn "security_context\s*=" --include="*.py" .
```

Produce the plan from grep output alone. **Do NOT read any files during Steps 1–3.** File reads are only permitted in Steps 4–6, immediately before editing a specific file.

---

### Step 2 — Strategy decision (from grep output, no reads needed)

**No file reads in this step or Step 3.** All decisions come from grep output only.

Apply this decision tree before writing the plan:

**Helm securityContext strategy:**

```
helper exists AND grep B returns nothing  → edit helper only (1 file, all workloads updated)
helper exists AND grep B returns files    → edit helper + patch listed files
no helper found                           → patch each workload template individually
```

**Dockerfile strategy:**

```
all final stages have USER >= 1000        → no Dockerfile changes needed
any final stage missing USER              → add USER 1000 before ENTRYPOINT/CMD
```

**Controller strategy:**

```
grep H returns nothing                    → no controller changes needed
grep H returns matches                    → read those files only, fix inline pod spec construction
                                            add getTmpVolume()/getTmpVolumeMount() to every pod
                                            use getDefaultContainerSecurityContext() or placeholder
```

**Writable paths strategy:**

```
grep G returns nothing                    → assume no emptyDir mounts needed; flag for runtime check
grep G returns matches                    → extract written paths from the grep G output lines directly
                                            (the matching line shows the path); do NOT read the entrypoint file.
                                            For well-known images (opensearch, telegraf, nginx, dashboards, etc.)
                                            use documented writable dirs from image docs, not file reads.
                                            For writes to config/bin dirs → apply config-template pattern
                                              (see Image Refactoring section).
                                            Flag any uncertain paths for runtime validation.
```

---

### Step 3 — Plan (required before any changes)

Write a plan using this fixed format, one block per service:

```
### <service-name>
- Dockerfile:            <change> | none needed
- Pod securityContext:   <change> | already compliant
- Container securityContext: <change> | already compliant
- Writable paths:        <emptyDir mounts or image refactor> | none needed | flagged for runtime check
- Controller:            <change> | n/a
```

At the top, state the Helm strategy chosen (helper edit / per-resource patch) and how many files it covers.

Ask:

> Proceed with applying these changes?

Do not apply changes before approval.

---

### Step 4 — Fix Dockerfiles

- Add `USER 1000` to final stage where missing, immediately before `ENTRYPOINT`/`CMD`
- Rename `config/` → `config-template/` in COPY instruction for images that will be refactored

### Step 5 — Apply securityContext

- Prefer shared helper (global change, minimal diff)
- Otherwise patch each workload identified in grep B
- Controller-built → fix inline pod spec construction in controller code

### Step 6 — Fix writable paths

- Only paths confirmed by grep G or known image docs
- Add emptyDir mounts; never omit `readOnlyRootFilesystem`

### Step 7 — Refactor images (config-template pattern)

For each image that wrote to root FS (found in grep G):

1. Dockerfile: rename `COPY <src>/config/ $HOME/config` → `COPY <src>/config/ $HOME/config-template`
2. Entrypoint: add init block (copy template to `/tmp/<svc>/config/`)
3. Replace all runtime writes throughout the entrypoint script
4. Update operator VolumeMount paths that pointed into the old config dir

### Step 8 — Add tests

**Go unit tests** (e.g. `controllers/provider/security_context_test.go`):

- `TestGetDefaultContainerSecurityContext` — readOnly=true, APE=false, drop=ALL
- `TestGetContainerSecurityContextReadOnly` / `TestGetContainerSecurityContextReadWrite`
- `TestGetTmpVolume` — emptyDir, sizeLimit=100Mi
- `TestGetTmpVolumeMount` — name=tmp, mountPath=/tmp

**Helm smoke tests** (`charts/helm/<chart>/tests/security_hardening_test.sh`):

- Run `helm template` with all major feature flags enabled
- Assert `grep -c 'readOnlyRootFilesystem: true'` > 0
- Assert `grep -c 'runAsNonRoot: true'` > 0
- Assert `grep -c 'allowPrivilegeEscalation: false'` > 0
- Assert `grep -c 'type: RuntimeDefault'` > 0
- Assert `grep -c '"ALL"'` > 0 (capabilities drop)
- Assert `grep -c 'name: tmp'` > 0 (emptyDir volumes)

---

## Validate

Run these after applying changes. Each check has a defined pass condition.

```bash
# Pass: no output (no forbidden fields present)
helm template . | grep -E "hostNetwork: true|hostPID: true|hostIPC: true"

# Pass: every workload block contains all three required fields
helm template . | grep -E "readOnlyRootFilesystem|runAsNonRoot|seccompProfile"

# Pass: no output (no forbidden ports)
helm template . | grep "containerPort" \
  | grep -E "(1[7-9][^0-9]|[2-9][0-9][^0-9]|[1-9][0-9]{2}[^0-9]|99[0-5]|1080|1236|143[34])"

# Pass: Go unit tests green
go test ./controllers/provider/... -v

# Pass: all smoke tests green
bash charts/helm/<chart>/tests/security_hardening_test.sh
```

Ensure:

- all required fields present in every workload
- no writes to root FS
- services start without permission errors

---

## Integration Test Service

### What to add to each service in docker-compose

If the repository ships a `demo/` directory with a docker-compose stack, it is
the fastest way to confirm that every hardened image actually starts under a
read-only root filesystem — **before** any Kubernetes deployment.

```yaml
services:
  <service>:
    read_only: true       # mounts the container root FS as read-only
    tmpfs:
      - /tmp              # writable scratch space (mirrors the emptyDir mount in Kubernetes)
    ...
```

That pair is the docker-compose equivalent of Kubernetes
`readOnlyRootFilesystem: true` + the `/tmp` emptyDir volume.

### Test-runner / integration-test containers — output volume, not tmpfs

Test-runner containers (e.g. Robot Framework, pytest wrappers) write their
reports and logs to a dedicated **output directory** (`/opt/robot/output` or
similar), not to `/tmp`.

- **Helm chart** — always mount a dedicated `emptyDir` at the output path.
  Do NOT rely on `/tmp` for this; test results would be lost between restarts
  and the emptyDir makes the intent explicit:

  ```yaml
  volumes:
    - name: output
      emptyDir: {}
  volumeMounts:
    - name: output
      mountPath: /opt/robot/output
  ```

- **docker-compose demo** — use a host bind-mount for the output path so
  results are accessible on the host after the run. Do NOT use `tmpfs` for
  the output directory, and do NOT add a `/tmp` tmpfs entry for this
  container. Set `PYTHONDONTWRITEBYTECODE=1` instead to suppress `.pyc`
  writes that would otherwise require a writable root FS or `/tmp`:

  ```yaml
  integration-tests:
    read_only: true # no tmpfs: entry needed
    volumes:
      - ./output:/opt/robot/output # host-accessible results
    environment:
      PYTHONDONTWRITEBYTECODE: "1"
  ```

---

## Common Pitfalls

1. **Nil values key comparison in Helm** — `eq .Values.SOME_KEY "value"` panics when the key
   is absent (nil is not a string). Fix: always use `default ""` first:
   `eq (default "" .Values.SOME_KEY) "value"`.

2. **ConfigMap volume mounts that landed in the config dir** — when the config dir moves from
   `$APP_HOME/config/` to `/tmp/<svc>/config/`, any operator `VolumeMount` with
   `MountPath: "<old-config>/subdir"` must be updated to `"/tmp/<svc>/config/subdir"`.

3. **Helm rendering fails on conditionally-referenced values** — helpers that compute derived
   values (e.g. disk capacity from `storage.size`) fail during `helm template` if the referenced
   value is not set. Pass `--set` overrides for any such values in smoke tests to prevent nil
   dereference errors during rendering.

4. **`chmod a+rw` does not make a path writable under `readOnlyRootFilesystem: true`** —
   permission bits are irrelevant once the filesystem is mounted read-only. Only a volume
   mount (emptyDir, PVC, etc.) makes a path writable at runtime.

5. **App config-path ENV declarations in Dockerfile are stale after entrypoint refactor** —
   Dockerfile `ENV` entries that point to config file paths inside `$APP_HOME/` (e.g.
   `CONFIG_FILE=/app/bin/tool.yml`) become wrong after the entrypoint moves those files to
   `/tmp/<svc>/`. Do not update the Dockerfile `ENV`; instead, `export` the new path at the
   very top of the entrypoint script so it overrides the image default.

6. **Conditional deletion of optional baked assets fails on read-only root FS** — if the
   original entrypoint deleted a baked directory when a feature was disabled (e.g.
   `rm -rf $APP_HOME/ui`), that `rm` fails with `readOnlyRootFilesystem: true`. Fix: invert
   the logic — conditionally **copy** the asset to `/tmp/<svc>/ui/` only when the feature is
   enabled, and reference that path. Never touch the read-only original.

7. **Stale documentation paths after entrypoint refactor** — when the config-template pattern
   moves runtime-generated files (CLI config, credential properties, tool config) from their
   original location (e.g. `$APP_HOME/bin/`) to `/tmp/<svc>/bin/`, public documentation and
   troubleshooting guides still reference the old paths.

   **Rule**: After any path move, do **both** of the following:

   a. **Create Dockerfile symlinks** for every file referenced in user-facing docs or used as
   a CLI argument. Place the symlink at the original path, pointing to the `/tmp/<svc>/…`
   target. Add them in a `RUN` layer after the grants block — symlinks are baked as
   read-only image data, so no runtime write is needed even with `readOnlyRootFilesystem: true`.
   The target is writable because it lives under the `/tmp` emptyDir mount.

   ```dockerfile
   # Keep documented CLI paths working after runtime files moved to /tmp/<svc>/bin/
   RUN ln -s /tmp/<svc>/bin/client.properties ${APP_HOME}/bin/client.properties \
       && ln -s /tmp/<svc>/bin/tool.yml ${APP_HOME}/bin/tool.yml
   ```

   b. **Update documentation** to show the canonical new path and add a brief note explaining
   the change and that the old relative path still works via the symlink. Replace all
   occurrences of the old path in code examples using `replace_all: true`.

   **When symlinks alone are sufficient** — if the file is only accessed programmatically
   (ENV vars, internal scripts) and never shown in docs, skip the doc update.

   **When doc updates alone are sufficient** — if the file was purely internal and never
   accessed via its old path by users or external scripts, skip the symlink.
