---
title: Check catalog
description: Ready-made RestoreChecks for common apps and their test status.
---

One folder per app. Each holds the same files:

- `restorecheck.yaml`: the RestoreCheck. Apply it with `kubectl apply -k checks/<app> -n <namespace>`.
- `seed.yaml`: test data for `hack/check-test/roundtrip.sh <app>`. Not for production.
- `README.md`: status, volumes, what it checks, canary, Secrets, UIDs and caveats.

## Test status (kind, Kopiur round trip, 2026-11-14/15)

A PASS means two things. The round trip passed. The negative test (broken canary) failed, as expected.
Harness: `hack/check-test/roundtrip.sh APP`. Details: [Test a check by hand](/guides/test-a-check/).

Every app below passed, except `minimal`, which is an example. Each README has the details.

| App | Status | Volumes (`sourcePVC` -> mount) | What it checks |
|---|---|---|---|
| [elasticsearch](https://github.com/linogics/bret-operator/tree/main/checks/elasticsearch) | PASS | `data` -> `/usr/share/elasticsearch/data` | Single-node start, cluster yellow, canary doc sha256. |
| [gitea](https://github.com/linogics/bret-operator/tree/main/checks/gitea) | PASS | `data` -> `/data` | `gitea web` boot, `/api/healthz` pass, canary repo raw file sha256. |
| [grafana](https://github.com/linogics/bret-operator/tree/main/checks/grafana) | PASS | `data` -> `/var/lib/grafana` | Server boot, `/api/health` database ok, canary dashboard via admin API. |
| [home-assistant](https://github.com/linogics/bret-operator/tree/main/checks/home-assistant) | PASS | `data` -> `/config` | Recorder DB integrity (read-only), canary file sha256, HTTP 401 from `/api/`. |
| [jellyfin](https://github.com/linogics/bret-operator/tree/main/checks/jellyfin) | PASS | `config` -> `/config`, `media` -> `/media` | `/health` Healthy, canary media file sha256. |
| [keycloak](https://github.com/linogics/bret-operator/tree/main/checks/keycloak) | PASS | `data` -> `/var/lib/postgresql/data` | `kc.sh start` against restored DB (Liquibase), `/health/ready` UP, canary realm OIDC discovery. |
| [mariadb](https://github.com/linogics/bret-operator/tree/main/checks/mariadb) | PASS | `data` -> `/var/lib/mysql` | Start mariadbd, `mariadb-check` on all DBs, canary row. |
| [minimal](https://github.com/linogics/bret-operator/tree/main/checks/minimal) | Example | `data` -> `/data` | The SHA-256 of the canary file matches the stored hash. |
| [mongodb](https://github.com/linogics/bret-operator/tree/main/checks/mongodb) | PASS | `data` -> `/data/db` | `mongod --fork` on restored dbpath, `validate full` on canary collection, canary sha256. |
| [mysql](https://github.com/linogics/bret-operator/tree/main/checks/mysql) | PASS | `data` -> `/var/lib/mysql` | Start mysqld on restored datadir, `mysqlcheck --check` on all DBs, canary row. |
| [n8n](https://github.com/linogics/bret-operator/tree/main/checks/n8n) | PASS | `data` -> `/home/node/.n8n` | Decrypted credential and workflow export contain the canary (proves `N8N_ENCRYPTION_KEY`), app `/healthz` ok. |
| [nextcloud](https://github.com/linogics/bret-operator/tree/main/checks/nextcloud) | PASS | `html` -> `/var/www/html`, `pgdata` -> `/var/lib/postgresql/data` | `occ status` (DB reachable), `occ integrity:check-core`, canary: `oc_filecache.size` equals the file size on disk. |
| [pihole](https://github.com/linogics/bret-operator/tree/main/checks/pihole) | PASS | `etc-pihole` -> `/etc/pihole`, `dnsmasq` -> `/etc/dnsmasq.d` | Gravity.db integrity and canary adlist, canary DNS line in custom.list, `pihole-FTL dnsmasq-test`. |
| [postgres](https://github.com/linogics/bret-operator/tree/main/checks/postgres) | PASS | `data` -> `/var/lib/postgresql/data` | Start with crash recovery, `pg_amcheck --heapallindexed` on every DB, canary row sha256. |
| [prometheus](https://github.com/linogics/bret-operator/tree/main/checks/prometheus) | PASS | `data` -> `/prometheus` | `promtool tsdb list`, server ready, `max_over_time(bret_canary_seq[3650d])` equals seeded value. |
| [rabbitmq](https://github.com/linogics/bret-operator/tree/main/checks/rabbitmq) | PASS | `data` -> `/var/lib/rabbitmq` | Server boot, `await_startup`, `check_running`, `check_local_alarms`, canary queue message count. |
| [redis](https://github.com/linogics/bret-operator/tree/main/checks/redis) | PASS | `data` -> `/data` | Server load (refuses corrupt RDB/AOF), canary key sha256. |
| [uptime-kuma](https://github.com/linogics/bret-operator/tree/main/checks/uptime-kuma) | PASS | `data` -> `/app/data` | Read-only `integrity_check` and canary monitor row via the app's own sqlite3 module, server boot, entry-page. |
| [vaultwarden](https://github.com/linogics/bret-operator/tree/main/checks/vaultwarden) | PASS | `data` -> `/data` | Python `integrity_check` (read-only), canary user row, app boots and `/alive` 200. |
| [wordpress](https://github.com/linogics/bret-operator/tree/main/checks/wordpress) | PASS | `html` -> `/var/www/html`, `mysql` -> `/var/lib/mysql` | MariaDB ping, canary post content equals sha256 of the uploaded file. |

Caveats that apply to every result:

- The restored mount roots come back owned by `root:65532`. Several checks change the owner of their mount in a root init container.
- Set pod-level settings, such as `securityContext.fsGroup`, in the check's pod fields. Most checks do not use `fsGroup`.
- Seeds contain test-only values. Their names start with `seed-...-not-secret`. A real check must use the source app's own Secrets.

Images were checked against public registries where noted. A `TAG UNVERIFIED` marker means the tag could not be confirmed at all.

## Conventions

- **restoredVolumes.** Every check lists its volumes as `{sourcePVC, mountPath}`. `sourcePVC` is the
  name of the PVC in the backed-up namespace. The Restore needs one source with the same `sourcePVC`
  for each entry. Single-PVC apps have one entry. Apps with two or more PVCs have one entry per PVC
  (nextcloud, wordpress, jellyfin, pihole). The table uses the seed PVC names. Rename `sourcePVC` to
  your PVC names.
- **Mount path = original mount path.** Restored volumes must mount at the path that the app used in
  production. For example: `/var/lib/postgresql/data` or `/data`. Absolute paths in the app config then
  still resolve. The example uses `/restore`. This is not required.
- **Canary contract.** Every data app must have a canary. The seeding job writes the canary before the
  backup. A canary is a row, file, doc or object. It holds a payload and its sha256. The check
  recomputes the hash on the restored data. This proves that the data is readable, not only present.
  The canary names are in each YAML header.
- **Native sidecars.** DB servers that an app needs at check time run as `initContainers` with
  `restartPolicy: Always` and a `startupProbe`. They keep running while the `containers` checks run.
  They require k8s 1.29+. A plain init container runs before the checks and exits; a native sidecar
  does not.
- **Secrets.** Checks read credentials from Secrets. Each YAML names its Secrets (`*-db`,
  `grafana-admin`, `n8n-secrets`). The operator or user must provision them with the source values.
  Encryption keys and admin passwords must match the source. If they do not match, the check fails for
  a reason that is unrelated to the backup.
- **Pod security.** The per-container `securityContext` sets the UID of each image: postgres 999,
  mysql 999, nextcloud and wordpress 33, grafana 472, prometheus 65534. Others use 1000 or the image
  default.
