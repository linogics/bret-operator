# RestoreCheck index

## Test status (kind, Kopiur round trip, 2026-11-14/15)

Each PASS means the round trip passed and the negative test (broken canary) failed as expected.
Harness: `hack/check-test/roundtrip.sh APP`. Details: `docs/testing-checks.md`.

| Result | Apps |
|---|---|
| PASS | postgres, mysql, mariadb, redis, mongodb, elasticsearch, rabbitmq, nextcloud, wordpress, gitea, grafana, prometheus, vaultwarden, home-assistant, jellyfin, uptime-kuma, keycloak, n8n, pihole |

Caveats that apply to the PASS results:

- Restored mount roots come back owned by `root:65532`. Several checks chown their mount in a root init container.
- Pod-level settings such as `securityContext.fsGroup` are set with the check's pod fields. Most checks still use no `fsGroup`.
- `rabbitmq` runs as `rabbit@localhost` in the test. The source node name is needed for a real check.
- `elasticsearch` heap is 512m in the test, to fit the test cluster.
- `keycloak` stops Postgres at pod end (kubelet), not with kill -9.
- Seeds contain test-only values, prefixed `seed-...-not-secret`. Real checks need the source app's own Secrets.

Images were checked against public registries where noted. A `TAG UNVERIFIED` marker means the tag could not be confirmed at all.

## Conventions

- **restoreMount vs restoredVolumes.** Single-PVC apps use `restoreMount`. Apps with two or more
  PVCs use `restoredVolumes` (DESIGN DELTA). Each restored volume is its own PVC, restored from the
  same backup point.
- **Mount path = original mount path.** Restored volumes mount at the path the app used in
  production (e.g. `/var/lib/postgresql/data`, `/data`). Absolute paths in app config then still
  resolve. Design §7.1 example uses `/restore`; this is not required.
- **Canary contract.** Every data app needs a canary written by the seeding job before the backup:
  a row, file, doc or object holding a payload plus its sha256. The check recomputes the hash on the
  restored data. This proves the data is readable and not just present. Canary names are in each YAML header.
- **Native sidecars (not in design §7.1).** DB servers that an app needs at check time run as
  `initContainers` with `restartPolicy: Always` and a `startupProbe`. They keep running while the
  `containers` checks run. Needs k8s 1.29+. Design §7.1 says initContainers run before checks and
  exit; this is a different use of the field and should be decided in the design.
- **Secrets.** Checks read credentials from Secrets named in each YAML (`*-db`, `grafana-admin`,
  `n8n-secrets`). The operator or user must provision them with the source values.
  Encryption keys and admin passwords must match the source, or the check fails for a
  reason unrelated to the backup.
- **Pod security.** Per-container `securityContext` sets the UID of each image (postgres 999, mysql
  999, nextcloud/wordpress 33, grafana 472, prometheus 65534, others 1000 or image default).

## Index

| app | data kind | volumes (PVC -> mount) | restore field used | check summary | open issues |
|---|---|---|---|---|---|
| postgres | database-only | data -> /var/lib/postgresql/data | restoreMount | crash-recovery start, `pg_amcheck --heapallindexed` on every DB, canary row sha256 | canary table `bret_canary` must exist in db `app`; backup must be crash-consistent; PGDATA owner uid 999 |
| mysql | database-only | data -> /var/lib/mysql | restoreMount | mysqld start on restored datadir, `mysqlcheck --check` all DBs, canary row | uid 999 assumed; performance_schema may print CHECK notes |
| mariadb | database-only | data -> /var/lib/mysql | restoreMount | mariadbd start, `mariadb-check` all DBs, canary row | restore must use the same MariaDB major as source |
| redis | database-only (RDB or AOF in /data) | data -> /data | restoreMount | server load (refuses corrupt RDB/AOF), canary key sha256 | AOF multipart format needs same redis major; canary keys must be written before backup |
| mongodb | database-only (WiredTiger) | data -> /data/db | restoreMount | `mongod --fork` on restored dbpath, `validate full` on canary collection, canary sha256 | TAG OK (mongo:7.0 probed); `mongosh` presence in the image NOT confirmed; replica-set dbs not covered |
| elasticsearch | files-only (Lucene in path.data) | data -> /usr/share/elasticsearch/data | restoreMount | single-node start, cluster yellow, canary doc sha256 | tag pulled OK in test; `cluster.name` and `node.name` must match source; host `vm.max_map_count` needed; uid 1000 |
| rabbitmq | files-only (mnesia dir + message store) | data -> /var/lib/rabbitmq | restoreMount | server boot, `await_startup`, `check_running`, `check_local_alarms`, canary queue message count | node name is tied to mnesia (`RABBITMQ_NODENAME`); `.erlang.cookie` must survive restore with mode 400 |
| nextcloud | database + files | html -> /var/www/html; pgdata -> /var/lib/postgresql/data | restoredVolumes + native sidecar postgres | `occ status` (DB reachable), `occ integrity:check-core`, canary: DB `oc_filecache.size` equals file size on disk | pdo_pgsql in image assumed; canary path assumes one storage; `dbhost` rewritten to localhost in config |
| wordpress | database + files | html -> /var/www/html; mysql -> /var/lib/mysql | restoredVolumes + native sidecar mariadb | mariadb ping, canary post content == sha256 of uploaded file | front-end render not checked; plugin-generated tables not covered |
| gitea | sqlite-in-volume (gitea.db + git repos) | data -> /data | restoreMount (/data) | `gitea web` boot, `/api/healthz` pass, canary repo raw file sha256 | SQLite only; postgres-backed installs need separate DB restore (not covered); repo and DB share one PVC so one snapshot |
| grafana | sqlite-in-volume (grafana.db) | data -> /var/lib/grafana | restoreMount | server boot, `/api/health` database ok, canary dashboard via admin API | admin password must match source; provisioning files from ConfigMap not included |
| prometheus | files-only (TSDB blocks + WAL) | data -> /prometheus | restoreMount | `promtool tsdb list`, server ready, `max_over_time(bret_canary_seq[3650d])` equals seeded value | canary metric must be ingested before backup; WAL replay time on large data |
| vaultwarden | sqlite-in-volume (db.sqlite3, attachments, icons) | data -> /data | restoreMount | python `integrity_check` (read-only), canary user row, app boots and `/alive` 200 | tag pulled OK in test; attachments are large; migrations run on a restored DB |
| home-assistant | sqlite-in-volume (home-assistant_v2.db) + .storage files | config -> /config | restoreMount | recorder DB integrity (read-only), canary file sha256, HTTP 401 from `/api/` | start may exceed time budget on slow nodes; recorder DB can be large |
| jellyfin | sqlite-in-volume (library.db, jellyfin.db) + media files | config -> /config; media -> /media | restoredVolumes | `/health` Healthy, canary media file sha256 | media PVC can be huge (option: restore config only); no sqlite3 in image, so DB integrity not checked |
| uptime-kuma | sqlite-in-volume (kuma.db) | data -> /app/data | restoreMount | read-only `integrity_check` and canary monitor row via app's own sqlite3 module, server boot, entry-page | 1.x schema only (2.x differs); uid 1000 assumed; entry-page grep is weak |
| keycloak | database-only (external PostgreSQL) | pgdata -> /var/lib/postgresql/data (sidecar) | restoreMount + native sidecar postgres | `kc.sh start` against restored DB (Liquibase), `/health/ready` UP, canary realm OIDC discovery | tag pulled OK in test; start may be slow; no separate realm export |
| n8n | sqlite-in-volume (database.sqlite) | data -> /home/node/.n8n | restoreMount | decrypted credential and workflow export contain canary (proves `N8N_ENCRYPTION_KEY`), app `/healthz` ok | encryption key lives in a Secret, not the volume; Postgres installs not covered |
| pihole | sqlite-in-volume (gravity.db) + config files | etc-pihole -> /etc/pihole; dnsmasq -> /etc/dnsmasq.d | restoredVolumes | gravity.db integrity and canary adlist, canary DNS line in custom.list, `pihole-FTL dnsmasq-test` | `dnsmasq-test` subcommand not confirmed in 2024.07; FTL DB schema tied to image version |

Tag status: probed OK on Docker Hub / quay: postgres:16.4, mysql:8.4, mariadb:11.4, redis:7.4,
mongo:7.0, rabbitmq:3.13-management, nextcloud:29-apache, wordpress:6.6-php8.3-apache,
gitea/gitea:1.22, grafana/grafana:11.2.0, vaultwarden/server:1.32.0,
homeassistant/home-assistant:2024.9.0, jellyfin/jellyfin:10.9.11, louislam/uptime-kuma:1.23.15,
n8nio/n8n:1.60.0, pihole/pihole:2024.07.0, prom/prometheus:v2.54.1.
Not confirmed: elasticsearch, python, keycloak (all marked in the YAML).

Popularity: the popularity list was used as given. No swaps were made. Artifact Hub / CNCF
usage was not checked.

## Apps that have data and a database

These apps keep state in two places: files and a DB, or a DB with a separate file store. The
two must come from the same backup point.

- **nextcloud** (files + postgres, two PVCs, restoredVolumes). `/var/www/html/data` holds the
  files. `oc_filecache` in postgres holds the rows that say which files exist and their size.
  If the DB comes from 10:00 and files from 10:05, rows point at missing files or files have no
  rows. Symptoms: broken file lists and "file not found" in the UI. `occ files:scan` can repair
  listings but may drop or duplicate entries. Handling: restore both PVCs from one backup selector
  in one attempt. Check: canary `oc_filecache.size` equals the file's size on disk.
- **wordpress** (files + mariadb, two PVCs, restoredVolumes). `wp_posts` rows reference uploads in
  `wp-content/uploads`. A DB newer than files means broken images. A DB older than files means
  orphan uploads. Handling: same as nextcloud, one attempt, both volumes. Check: canary post content
  equals the sha256 of the uploaded canary file.
- **jellyfin** (config sqlite + media files, two PVCs, restoredVolumes). `library.db` stores
  media item paths and IDs. If media is from a different point, items show as missing or
  unavailable, and metadata is wrong. Handling: restore config and media from one attempt, mount
  media at its original path. Check: canary media file hash; `/health`. Option: restore config
  only if media is too large, and accept that the check covers the DB only.
- **pihole** (gravity.db + dnsmasq.d, two PVCs, restoredVolumes). Adlists and custom DNS live in
  `gravity.db` and `custom.list`. Config files in `/etc/dnsmasq.d` must match the FTL version. A mix
  causes lists that are in the DB but not applied, or DNS records that are missing. Check: canary
  adlist row in gravity.db, canary line in custom.list, dnsmasq config parses.
- **keycloak** (postgres only, one PVC, native sidecar). No file store in this check. If
  themes or providers live on a second PVC, they must be restored with the DB (add restoredVolumes).
  Check: realm OIDC discovery and `/health/ready`.
- **n8n** (sqlite, one PVC, encryption key in a Secret). The DB stores credentials encrypted with
  `N8N_ENCRYPTION_KEY`. A DB restored with a different key cannot decrypt credentials, and
  workflows break silently. Check: decrypted export contains the canary credential. Failure here means key
  mismatch, not a bad backup.
- **gitea, grafana, home-assistant, vaultwarden, uptime-kuma** (sqlite in one PVC). The DB and
  files share one PVC, so one snapshot covers both. The risk here is the sqlite file
  itself: the backup must be taken with the app stopped or with a consistent snapshot. Checks: DB
  integrity and canary row or file.
- **postgres, mysql, mariadb, redis, mongodb** (database only). The check reads the DB's own
  structure (`pg_amcheck`, `mysqlcheck`, `validate`) plus the canary row. A physical copy taken while the DB
  is running is recovered by crash recovery, which is part of the test.
- **elasticsearch, prometheus, rabbitmq** (files only, one PVC). Each has its own on-disk
  format. The checks rely on the server loading the restored data and answering a query for the
  canary.
