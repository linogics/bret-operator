# Writing RestoreChecks

A `RestoreCheck` is the check. It is a pod template. The operator creates a pod from it,
mounts the restored volume, and runs it. The pod's exit code is the verdict. The operator does
not read logs.

- Exit code 0 from every check container: the check passes.
- Any other exit code: the attempt fails.

You may use any image and any language: bash, Python, a Go binary, `psql`, `php occ`, `mysqlcheck`,
or anything else that exits with a code. The check runs in its own image, not in the application image.

Start with [examples/restorecheck-minimal.yaml](../examples/restorecheck-minimal.yaml).
It is the smallest check that works, and it is tested.

## Anatomy

```yaml
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreCheck
metadata:
  name: orders-db-check
  namespace: data
spec:
  restoreMount: /var/lib/postgresql/data     # one restored volume, mounted here
  initContainers: []                         # run first, in order
  containers:                                # the checks; all must exit 0
    - name: check
      image: postgres:17.12
      command: ["/bin/sh", "-c", "pg_isready --version && ..."]
  scratchVolumes: []                         # extra empty volumes, one per attempt
  volumes: []                                # ConfigMaps and Secrets the pod needs
```

| Field | Required | Meaning |
|---|---|---|
| `restoreMount` | one of `restoreMount` or `restoredVolumes` | Mount path of the single restored volume. It is mounted in every init and check container. |
| `restoredVolumes` | one of the two | List of `{name, mountPath}` for apps with several volumes. Each entry is its own restored volume. |
| `initContainers` | no | Run before the checks. A native sidecar (see below) also goes here. |
| `containers` | yes, at least one | The check containers. Exit code 0 means pass. |
| `scratchVolumes` | no | `{name, mountPath, storageClassName, size}`. Empty PVCs created per attempt. |
| `volumes` | no | Extra pod volumes, for example a ConfigMap with a check script. |

Set exactly one of `restoreMount` and `restoredVolumes`. The API rejects both or neither.

## Verdict rules

- Pod `Succeeded`: pass. Pod `Failed`: fail. The operator reads the pod phase.
- An init container that exits non-zero fails the attempt before the checks run.
- A check that hangs is stopped by `restoreWindow` (see [restoretest-guide.md](restoretest-guide.md)).
- Test each check with a broken input. Make the expected value wrong once, and confirm exit 1.
  The spike did this, and the check failed as it should.

## One volume: restoreMount

Use `restoreMount` when the application has one volume.

Key rule: the mount path must be the path the application used in production. Absolute paths in
the application's config then still resolve on the restored copy.

## Several volumes: restoredVolumes

Use `restoredVolumes` when the application has two or more volumes, for example a web root
plus a database directory:

```yaml
spec:
  restoredVolumes:
    - name: html                 # the restored volume's key, see below
      mountPath: /var/www/html
    - name: mysql
      mountPath: /var/lib/mysql
```

How the names map to volumes: when you test with `hack/check-test`, the restored PVC for
the key `html` is named `r-html`. The operator's name mapping is planned with the controller.

## Scratch volumes

Some checks need space that the restore does not give them. Examples: unpacking a dump, or a
database that writes temporary files outside the restored data.

```yaml
spec:
  scratchVolumes:
    - name: work
      mountPath: /work
      storageClassName: fast-ssd   # empty = cluster default
      size: 20Gi
```

Rules (design §7.1):

- The operator creates one PVC per scratch volume per attempt. The `Restore` owns the PVC.
- The operator deletes scratch PVCs with the attempt. With `keepOnFailure`, it keeps them.
- `size` is required. Set it to what the check needs, not what the source had.
- The restored volume's default size is the source volume's capacity. A restore into a volume
  that is too small fails early with `InsufficientCapacity` (planned).

## Native sidecars

A check often needs a database server that runs next to it. For example, Keycloak or Nextcloud
check against Postgres. Use a native sidecar: an init container with `restartPolicy: Always`.
It starts before the checks and keeps running while they run. Kubernetes 1.29 or newer is
required.

```yaml
spec:
  initContainers:
    - name: postgres
      image: postgres:17.12
      restartPolicy: Always
      startupProbe:
        exec:
          command: ["/bin/sh", "-c", "pg_isready -h /tmp"]
        periodSeconds: 2
        failureThreshold: 60
```

Design §7.1 describes `initContainers` as run-to-completion. Native sidecars are an extension
that the checks use. The design needs a decision before the controller ships.

## Canary convention

A check that only starts the app proves little. The canary proves that the data is readable and
unchanged.

1. Before the backup, the seed writes a canary: a row, file, document or object, with a
   payload and the SHA-256 of that payload.
2. The check reads the canary from the restored data and recomputes the SHA-256.
3. The check passes only if the hashes match.

Postgres example (from [checks/postgres.yaml](../checks/postgres.yaml)):

```sql
select count(*) from bret_canary
where id = 1 and encode(sha256(payload::bytea), 'hex') = sha256;
```

Name the canary in the header comment of your check. Put the seed for it in
`hack/check-test/seeds/`.

## Secrets

The check needs some credentials. For example: a database password, an admin token, or an encryption key.
Take them from a Secret in the `RestoreTest`'s namespace. Reference it with `env[].valueFrom.secretKeyRef`.

Values must match the source. An encryption key or admin password that differs makes the check
fail for a reason unrelated to the backup. Keep one Secret per application. Make the backup
and the check read the same one.

## Pod security

Set `securityContext.runAsUser` on each container to the UID that the image runs as. Check the image
with `docker run --rm <image> id`, or read the image's documentation.

Two rules from the spike:

- **Do not set `fsGroup` on a database data directory.** `fsGroup` changes the mode to `2770`. Postgres
  refuses to start on that mode. Set `fsGroup` only on application writers that need it.
- **Postgres family: set mode `0700` in an init container.** The checks in `checks/`
  run a `pgdata-perms` init container as UID 999. It checks that `PG_VERSION` exists, and runs
  `chmod 0700 "$PGDATA"`. Your data must live in `<mount>/pgdata`, which is the `PGDATA` in the
  check.

The backup side needs the same UID. See [backup-providers.md](backup-providers.md) for the
mover security context of each tool.

## Images and tags

- Pin a tag. Do not use `latest`.
- The check image should match the major version of the app. A database restored into a different
  major version may not start.
- Confirm that the tag exists before you use it. `hack/check-test` pulls the image as part of
  its run.

## Testing a check

Read [testing-checks.md](testing-checks.md). Short version:

```bash
# Run a check against restored PVCs you already have
hack/check-test/check.sh examples/restorecheck-minimal.yaml restore-test check-minimal data=my-restored-pvc

# Or run the whole round trip: seed, back up with Kopiur, restore, check
hack/check-test/roundtrip.sh minimal hack/check-test/seeds/minimal.yaml examples/restorecheck-minimal.yaml
```

## Catalog

[checks/INDEX.md](../checks/INDEX.md) lists the app checks, the volumes each one
needs, the canary it uses, and its test status.
