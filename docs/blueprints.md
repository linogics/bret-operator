# Writing and using RestoreBlueprints

Status: design. The `RestoreBlueprint` type is a scaffold in the code. All blueprints in
`blueprints/` are **UNTESTED**. None were run. Test each one on a kind cluster before use.

## What a blueprint is

A `RestoreBlueprint` is a `PodSpec`-shaped template. The operator creates a pod from it,
attaches the restored volume, and runs the pod. Pod exit code is the verdict:

- Every check container exits 0: the check passes.
- Any non-zero exit: the attempt fails (design §7.1, §10).

The operator does not read logs or parse output. Only exit codes count.

Any image and any language work. Bash, Python, a Go binary, `psql`, `php occ`, or anything else.

## Single volume: restoreMount

Use `restoreMount` for an app with one PVC.

```yaml
apiVersion: restore.bret.linogics.io/v1alpha1
kind: RestoreBlueprint
metadata:
  name: postgres
  namespace: default
spec:
  restoreMount: /var/lib/postgresql/data
  containers:
  - name: check
    image: postgres:16.4
    command: [/bin/sh, -c, "..."]
```

The restored volume is mounted at `restoreMount` in every container.

## Several volumes: restoredVolumes (design delta, proposed)

Use `restoredVolumes` for an app with two or more PVCs. Each restored volume is its own
PVC, restored from the same backup point. Mount each one at the path the app used in
production. Then absolute paths in the app config still work.

```yaml
spec:
  restoredVolumes:
  - name: html
    mountPath: /var/www/html
  - name: pgdata
    mountPath: /var/lib/postgresql/data
```

Design §7.1 does not have this field. See [crds.md](crds.md#restoredvolumes-design-delta-proposed).

## Init containers and sidecars

- `initContainers` run before the check containers, per design §7.1.
- The blueprints also use init containers as native sidecars: `restartPolicy: Always`
  plus a `startupProbe`. The sidecar keeps running while the checks run. This needs
  Kubernetes 1.29 or later. The design does not have this yet. Used by `nextcloud.yaml`
  (postgres) and `keycloak.yaml`, and mentioned for `wordpress` and `postgres`-backed apps.
- Use a native sidecar when the check needs a database server running next to it.

## Canary convention

Every data app needs a canary. This proves the data is readable, not just present.

1. Before the backup, the seeding job writes a canary. It is a row, file, document or
   object. It holds a payload and the sha256 of that payload.
2. The check reads the canary from the restored data and recomputes the sha256.
3. The check passes only when the recomputed hash matches the stored one.

Examples from the blueprints:

- Postgres: table `bret_canary(id int, payload text, sha256 text)` in db `app`. Row `id=1`.
  The check runs `encode(sha256(payload::bytea), 'hex')` and compares it to `sha256`.
- Nextcloud: the DB size of `files/bret-canary.txt` in `oc_filecache` must equal the file
  size on disk.
- Other blueprints use the same idea with their own table, file or object.

Canary names are written in the header comment of each YAML.

## Negative check

Test that a check can fail. Set the expected value wrong and confirm exit code 1. The spike
did this with a wrong expected count, and the check exited 1. See
[spike-findings.md](spike-findings.md).

## Secrets

Blueprints read credentials from Secrets named in each YAML (for example `nextcloud-db`,
`grafana-admin`, `minio-creds`, `n8n-secrets`). Provision them with the values from the
source app. Encryption keys and admin passwords must match the source. If not, the check
fails for a reason that has nothing to do with the backup.

## Pod security

Each container sets `securityContext` to the UID the image uses. From `INDEX.md`: postgres
999, mysql 999, nextcloud and wordpress 33, grafana 472, prometheus 65534, others 1000 or
the image default.

Do not set `fsGroup` on a database data directory. See
[best-practices.md](best-practices.md) and the spike facts.

**Known conflicts in the current blueprints:** `postgres.yaml` and `nextcloud.yaml`
set `fsGroup: 999` on the database containers. The spike showed this breaks Postgres
startup on a restored data dir. Remove `fsGroup` from those containers, or chmod the data dir
to 0700 in an init step, before running them.

## Using blueprints/ in this repo

- `blueprints/INDEX.md` lists 20 apps. Each row gives the data kind, the volumes, the
  restore field used, the check summary and open issues.
- One YAML per app: `blueprints/<app>.yaml`.
- Header comments: `# UNTESTED`, the canary contract, and any design delta.
- To use one: check the image tag, provision the Secrets, fix the `fsGroup` issue
  if the app is a database, then apply the `RestoreBlueprint` to the namespace of the
  `RestoreTest`.
- Only admins may create `RestoreBlueprint` objects. See the security section in the design.

Tag status (from `INDEX.md`): many tags probed OK on Docker Hub or quay. These are not
confirmed: elasticsearch, minio, minio/mc, python, keycloak.

Note: `postgres.yaml` uses `postgres:16.4`. The spike used Postgres 17.12.
The spike result does not cover 16.4.

## Postgres data dirs: the pgdata-perms init step

Restore can leave the Postgres data dir wider than Postgres accepts (2770 after an
`fsGroup` on the mount). Postgres then refuses to start. The Postgres-family blueprints
(`postgres.yaml`, `keycloak.yaml`, `nextcloud.yaml`) run a `pgdata-perms` init container
first. It runs as uid 999, checks `PG_VERSION` exists, and sets the data dir to 0700.

The source must keep its data in `<mount>/pgdata`, matching `PGDATA` in the blueprint.
These blueprints are UNTESTED. The init step is a convention until the operator does it.
