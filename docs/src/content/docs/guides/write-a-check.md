---
title: Write a check
description: Write a RestoreCheck that proves restored data is usable.
---

A `RestoreCheck` is the check. It is a pod template. The operator creates a pod from it,
mounts the restored volume, and runs it. The pod's exit code is the verdict. The operator does
not read logs.

- Exit code 0 from every check container: the check passes.
- Any other exit code: the attempt fails.

You may use any image and any language: bash, Python, a Go binary, `psql`, `php occ`, `mysqlcheck`,
or anything else that exits with a code. The check runs in its own image, not in the application image.

Start with [checks/wordpress/restorecheck.yaml](https://github.com/linogics/bret-operator/blob/main/checks/wordpress/restorecheck.yaml): an app with
two volumes. [checks/minimal/restorecheck.yaml](https://github.com/linogics/bret-operator/blob/main/checks/minimal/restorecheck.yaml) is the
short form with one volume. Both work.

## Anatomy

```yaml
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreCheck
metadata:
  name: nextcloud
  namespace: nextcloud
spec:
  restoredVolumes:                           # one entry per restored PVC
    - sourcePVC: html                        # PVC in the backed-up namespace
      mountPath: /var/www/html
    - sourcePVC: pgdata
      mountPath: /var/lib/postgresql/data
  initContainers: []                         # run first, in order
  containers:                                # the checks; all must exit 0
    - name: check
      image: nextcloud:29-apache
      command: ["/bin/sh", "-c", "php occ status && ..."]
  scratchVolumes: []                         # extra empty volumes, one per attempt
  volumes: []                                # ConfigMaps and Secrets the pod needs
```

| Field | Required | Meaning |
|---|---|---|
| `restoredVolumes` | yes, at least one | List of `{sourcePVC, mountPath}`. Each restored volume is mounted in every init and check container. See below. |
| `initContainers` | no | Run before the checks. A native sidecar (see below) also goes here. |
| `containers` | yes, at least one | The check containers. Exit code 0 means pass. |
| `scratchVolumes` | no | `{name, mountPath, storageClassName, size}`. Empty PVCs created per attempt. |
| `volumes` | no | Extra pod volumes, for example a ConfigMap with a check script. |


The diagram shows the check pod that the operator builds from these fields. The example is a
nextcloud check with a `pgdata-perms` init container and a `postgres` sidecar.

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
     html, pgdata      at their mountPath, in every container
     scratchVolumes    one empty PVC per attempt
     volumes           your ConfigMaps and Secrets
```

## Verdict rules

- Pod `Succeeded`: pass. Pod `Failed`: fail. The operator reads the pod phase.
- An init container that exits non-zero fails the attempt before the checks run.
- A check that hangs is stopped by `restoreWindow` (see [Schedule restore tests](/guides/restore-tests/)).
- Test each check with a broken input. Make the expected value wrong once, and confirm exit 1.

## Restored volumes

Each `restoredVolumes` entry has two fields:

- `sourcePVC`: the PVC in the backed-up namespace that this entry restores. It must be unique in
  the check.
- `mountPath`: where the restored volume is mounted.

A `Restore` or `RestoreTest` must have one source with the same `sourcePVC` for every entry. If
the sets differ, the `Restore` ends Failed with `SourceMissing`. See
[Volumes and scratch space](/concepts/volumes/#which-pvc-gets-restored).

Key rule: the mount path must be the path the application used in production. Absolute paths in
the application's config then still resolve on the restored copy.

A single-volume app has one entry:

```yaml
spec:
  restoredVolumes:
    - sourcePVC: data
      mountPath: /var/lib/app
```

All volumes come from the same backup point. For a database plus files this is the point: see
[Data and database](/guides/data-and-database/).

## Mover UID

The restore mover writes the volume as some UID. The annotation `bret.linogics.io/mover-uid` sets it
for each volume, keyed by `sourcePVC`. Without a key, the first container's `runAsUser` is used.

```yaml
metadata:
  annotations:
    bret.linogics.io/mover-uid: '{"html": 33, "pgdata": 999}'
```

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
  restored volume only. See [Volumes and scratch space](/concepts/volumes/#scratch-volume-size).

## Native sidecars

A check often needs a database server that runs next to it. For example, Keycloak or Nextcloud
check against Postgres. Use a native sidecar: an init container with `restartPolicy: Always`, as
`postgres` in the diagram under [Anatomy](/guides/write-a-check/#anatomy). Kubernetes 1.29 or newer is required.

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

Postgres example (from [checks/postgres/restorecheck.yaml](https://github.com/linogics/bret-operator/blob/main/checks/postgres/restorecheck.yaml)):

```sql
select count(*) from bret_canary
where id = 1 and encode(sha256(payload::bytea), 'hex') = sha256;
```

Name the canary in the header comment of your check. Put the seed for it next to the
check, in `checks/<app>/seed.yaml`.

## Secrets

A check can read a Secret, for example for a database password, an admin token, or an encryption
key. The diagram shows where the check pod runs, and so where the Secret must be. Here your
namespace is `data`.

```d2 title="Where the check pod runs and where its Secret must be, per provider"
grid-columns: 1
one: "Kopiur, VolSync: one namespace" {
  data: data {
    grid-columns: 3
    rt: RestoreTest
    rc: RestoreCheck
    r: Restore
    pod: check pod
    secret: Secret
  }
  data.pod -> data.secret: reads
}
two: "Velero: two namespaces" {
  direction: down
  data: data {
    grid-columns: 2
    rt: RestoreTest
    rc: RestoreCheck
    r: Restore
    secret: Secret
  }
  scratch: "bret-scratch-<hash>" {
    pod: check pod
    secret: Secret
    pod -> secret: reads
  }
  data.secret -> scratch.secret: copied by operator
}
```

For every provider, create the Secret in the namespace of your `RestoreTest`. You do not need
to know a target namespace in advance. With Velero, the operator copies the Secrets and ConfigMaps
that `imagePullSecrets`, `env` and `envFrom` name into the scratch namespace.

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
      image: postgres:17.12
      env:
        - name: PGPASSWORD              # one key
          valueFrom:
            secretKeyRef:
              name: nextcloud-db     # Secret in the RestoreTest namespace
              key: password
      envFrom:                          # or every key of the Secret
        - secretRef:
            name: nextcloud-db
```

For a file, put the Secret in `spec.volumes` and mount it in the container with `volumeMounts`.
The operator mounts only the restored volumes and the scratch volumes for you.

Rules:

- With Kopiur and VolSync, the operator does not check that the Secret exists. A missing Secret
  keeps the check pod from starting. When the `restoreWindow` expires, the attempt ends as
  `Canceled`. With Velero, a missing Secret ends the attempt as `Failed` with `CheckRefMissing`.
- Values must match the source. An encryption key or admin password that differs makes the check
  fail for a reason unrelated to the backup. Copy the Secret from the source namespace and keep
  its values. Keep one Secret per application, and make the backup and the check read the same one.
- With Velero, the operator copies only Secrets and ConfigMaps named in `imagePullSecrets`, `env`
  and `envFrom`. Secret and ConfigMap volumes are rejected with `ProviderUnsupported`, and so are
  pull secrets that only the `default` ServiceAccount lists. The full list of what a Velero check
  must not set is in [Velero walkthrough](/providers/velero/walkthrough/#3-write-the-check).

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

The backup side needs the same UID. See [providers/](/providers/) for the
mover security context of each tool.

## Images and tags

- Pin a tag. Do not use `latest`.
- The check image should match the major version of the app. A database restored into a different
  major version may not start.
- Confirm that the tag exists before you use it. The check pod pulls the image when it runs.

## Testing a check

Read [Test a check by hand](/guides/test-a-check/). Short version:

```bash
# Run a check against restored PVCs you already have
hack/check-test/check.sh checks/minimal/restorecheck.yaml restore-test check-minimal data=my-restored-pvc

# Or run the whole round trip: seed, back up with Kopiur, restore, check
hack/check-test/roundtrip.sh minimal checks/minimal/seed.yaml checks/minimal/restorecheck.yaml
```

## Catalog

[Check catalog](/guides/check-catalog/) lists the app checks, the volumes each one
needs and the canary it uses.
