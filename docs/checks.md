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
| `restoredVolumes` | one of the two | List with one `{name, mountPath, sourcePVC}` entry in v1. See below. |
| `initContainers` | no | Run before the checks. A native sidecar (see below) also goes here. |
| `containers` | yes, at least one | The check containers. Exit code 0 means pass. |
| `scratchVolumes` | no | `{name, mountPath, storageClassName, size}`. Empty PVCs created per attempt. |
| `volumes` | no | Extra pod volumes, for example a ConfigMap with a check script. |

Set exactly one of `restoreMount` and `restoredVolumes`. The API rejects both or neither.

The diagram shows the check pod that the operator builds from these fields. The example is a
Postgres check with a `pgdata-perms` init container and a `postgres` sidecar.

```text
check pod <restore>-check
│
├─ initContainers, in order
│    pgdata-perms          runs, exits 0
│    postgres (sidecar)    restartPolicy: Always, keeps running
│
├─ containers              all must exit 0 = pass
│    check
│
└─ volumes
     restored volume   at restoreMount, in every container
     scratchVolumes    one empty PVC per attempt
     volumes           your ConfigMaps and Secrets
```

## Verdict rules

- Pod `Succeeded`: pass. Pod `Failed`: fail. The operator reads the pod phase.
- An init container that exits non-zero fails the attempt before the checks run.
- A check that hangs is stopped by `restoreWindow` (see [restoretest-guide.md](restoretest-guide.md)).
- Test each check with a broken input. Make the expected value wrong once, and confirm exit 1.

## One volume: restoreMount

Use `restoreMount` when the application has one volume.

Key rule: the mount path must be the path the application used in production. Absolute paths in
the application's config then still resolve on the restored copy.

## One entry: restoredVolumes

`restoredVolumes` takes one entry in v1. Each entry names the restored volume, the mount path,
and the PVC in the backed-up namespace that it restores (`sourcePVC`):

```yaml
spec:
  restoredVolumes:
    - name: data                 # the key for hack/check-test/check.sh
      mountPath: /var/lib/app
      sourcePVC: app-data        # PVC in the backed-up namespace
```

The operator rejects a check with two or more entries. The check then fails with
`ProviderUnsupported`. Checks for apps with several volumes cannot run in the operator in v1. See
[data-and-database.md](data-and-database.md). Multi-entry support is not in v1.

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

Rules:

- The operator creates one PVC per scratch volume per attempt. The `Restore` owns the PVC.
- The operator deletes scratch PVCs with the attempt. With `keepOnFailure`, it keeps them.
- `size` is required. Set it to what the check needs, not what the source had.
- The operator does not compare `size` with the source. `InsufficientCapacity` is about the
  restored volume only. See [concepts.md](concepts.md#scratch-volume-size).

## Native sidecars

A check often needs a database server that runs next to it. For example, Keycloak or Nextcloud
check against Postgres. Use a native sidecar: an init container with `restartPolicy: Always`, as
`postgres` in the diagram under [Anatomy](#anatomy). Kubernetes 1.29 or newer is required.

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

A check can read a Secret, for example for a database password, an admin token, or an encryption
key. The diagram shows where the check pod runs, and so where the Secret must be. Here your
namespace is `data`.

```text
Kopiur, VolSync: one namespace         Velero: two namespaces

┌ data ───────────────────────┐        ┌ data ───────────────┐
│ RestoreTest                 │        │ RestoreTest         │
│ RestoreCheck                │        │ RestoreCheck        │
│ Restore                     │        │ Restore             │
│ check pod ──reads─► Secret  │        └─────────────────────┘
└─────────────────────────────┘        ┌ bret-scratch-<hash> ┐
                                       │ check pod           │  Secrets are rejected:
                                       └─────────────────────┘  ProviderUnsupported
```

For Kopiur and VolSync, create the Secret in the namespace of your `RestoreTest`. You do not need
to know a target namespace in advance.

Find the Secrets a check needs before you apply it:

```bash
grep -n -A3 'secretKeyRef\|secretRef\|secret:' check.yaml
```

The easiest source is the workload that runs the app. Its `env` and `envFrom` already name the
Secret that it uses.

Reference a Secret in the container:

```yaml
spec:
  containers:
    - name: check
      image: postgres:17
      env:
        - name: PGPASSWORD              # one key
          valueFrom:
            secretKeyRef:
              name: orders-db-check     # Secret in the RestoreTest namespace
              key: password
      envFrom:                          # or every key of the Secret
        - secretRef:
            name: orders-db-check
```

For a file, put the Secret in `spec.volumes` and mount it in the container with `volumeMounts`.
The operator mounts only the restored volume and the scratch volumes for you.

Rules:

- The operator does not check that the Secret exists. A missing Secret keeps the check pod from
  starting. When the `restoreWindow` expires, the attempt ends as `Canceled`.
- Values must match the source. An encryption key or admin password that differs makes the check
  fail for a reason unrelated to the backup. Copy the Secret from the source namespace and keep
  its values. Keep one Secret per application, and make the backup and the check read the same one.
- Velero does not support Secrets in checks. The operator rejects the check with
  `ProviderUnsupported`. The full list of what a Velero check must not set is in
  [concepts.md](concepts.md#velero).

## Pod security

The operator sets safe defaults on every check pod. Any field you set yourself wins, so you can
change a default when a check needs it:

| Field | Default | Overwrite it with |
|---|---|---|
| `automountServiceAccountToken` (on the RestoreCheck) | `false` | `true` |
| `securityContext.seccompProfile` (pod) | `RuntimeDefault` | any profile |
| `securityContext.allowPrivilegeEscalation` (container) | `false` | `true` |
| `securityContext.privileged` (container) | `false` | `true` |

A `hostPath` volume is rejected unless the RestoreCheck has the annotation
`bret.linogics.io/allow-host-path: "true"`. The Velero provider never allows it.

Defaults do not drop Linux capabilities. Several checks run `chown` in an init container, and that
needs `CAP_CHOWN`. A check that does not need capabilities can set `capabilities.drop: [ALL]`.

Set `securityContext.runAsUser` on each container to the UID that the image runs as. Check the image
with `docker run --rm <image> id`, or read the image's documentation.

Two rules for database checks:

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
- Confirm that the tag exists before you use it. The check pod pulls the image when it runs.

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
