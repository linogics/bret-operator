# RestoreCheck index

## Test status (kind, Kopiur round trip, 2026-11-14/15)

A PASS means two things. The round trip passed. The negative test (broken canary) failed, as expected.
Harness: `hack/check-test/roundtrip.sh APP`. Details: `docs/testing-checks.md`.

| Result | Apps |
|---|---|
| PASS | postgres, mysql, mariadb, redis, mongodb, elasticsearch, rabbitmq, nextcloud, wordpress, gitea, grafana, prometheus, vaultwarden, home-assistant, jellyfin, uptime-kuma, keycloak, n8n, pihole |

Caveats that apply to the PASS results:

- The restored mount roots come back owned by `root:65532`. Several checks change the owner of their mount in a root init container.
- Set pod-level settings, such as `securityContext.fsGroup`, in the check's pod fields. Most checks do not use `fsGroup`.
- `rabbitmq` runs as `rabbit@localhost` in the test. A real check must use the source node name.
- The `elasticsearch` heap is 512m in the test. This fits the test cluster.
- `keycloak` stops Postgres at pod end through the kubelet. It does not use kill -9.
- Seeds contain test-only values. Their names start with `seed-...-not-secret`. A real check must use the source app's own Secrets.

Images were checked against public registries where noted. A `TAG UNVERIFIED` marker means the tag could not be confirmed at all.

## Conventions

- **restoreMount vs restoredVolumes.** Single-PVC apps must use `restoreMount`. Apps with two or more
  PVCs must use `restoredVolumes` (DESIGN DELTA). Each restored volume is a separate PVC. All volumes
  are restored from the same backup point.
- **Mount path = original mount path.** Restored volumes must mount at the path that the app used in
  production. For example: `/var/lib/postgresql/data` or `/data`. Absolute paths in the app config then
  still resolve. The design §7.1 example uses `/restore`. This is not required.
- **Canary contract.** Every data app must have a canary. The seeding job writes the canary before the
  backup. A canary is a row, file, doc or object. It holds a payload and its sha256. The check
  recomputes the hash on the restored data. This proves that the data is readable, not only present.
  The canary names are in each YAML header.
- **Native sidecars (not in design §7.1).** DB servers that an app needs at check time run as
  `initContainers` with `restartPolicy: Always` and a `startupProbe`. These native sidecars keep running
  while the `containers` checks run. They require k8s 1.29+. Design §7.1 says that initContainers run
  before the checks and exit. This is a different use of the field. The design should decide it.
- **Secrets.** Checks read credentials from Secrets. Each YAML names its Secrets (`*-db`,
  `grafana-admin`, `n8n-secrets`). The operator or user must provision them with the source values.
  Encryption keys and admin passwords must match the source. If they do not match, the check fails for
  a reason that is unrelated to the backup.
- **Pod security.** The per-container `securityContext` sets the UID of each image: postgres 999,
  mysql 999, nextcloud and wordpress 33, grafana 472, prometheus 65534. Others use 1000 or the image
  default.

## Index

| app | data kind | volumes (PVC -> mount) | restore field used | check summary | open issues |
|---|---|---|---|---|---|
| postgres | database-only | data -> /var/lib/postgresql/data | restoreMount | start with crash recovery, `pg_amcheck --heapallindexed` on every DB, canary row sha256 | Canary table `bret_canary` must exist in db `app`. The backup must be crash-consistent. The PGDATA owner must be uid 999. |
| mysql | database-only | data -> /var/lib/mysql | restoreMount | start mysqld on restored datadir, `mysqlcheck --check` on all DBs, canary row | UID 999 is assumed. performance_schema may print CHECK notes. |
| mariadb | database-only | data -> /var/lib/mysql | restoreMount | start mariadbd, `mariadb-check` on all DBs, canary row | The restore must use the same MariaDB major version as the source. |
| redis | database-only (RDB or AOF in /data) | data -> /data | restoreMount | server load (refuses corrupt RDB/AOF), canary key sha256 | The AOF multipart format needs the same redis major version. Write the canary keys before the backup. |
| mongodb | database-only (WiredTiger) | data -> /data/db | restoreMount | `mongod --fork` on restored dbpath, `validate full` on canary collection, canary sha256 | TAG OK (mongo:7.0 probed). `mongosh` presence in the image NOT confirmed. Replica-set DBs are not covered. |
| elasticsearch | files-only (Lucene in path.data) | data -> /usr/share/elasticsearch/data | restoreMount | single-node start, cluster yellow, canary doc sha256 | The tag was pulled OK in the test. `cluster.name` and `node.name` must match the source. The host needs `vm.max_map_count`. UID 1000. |
| rabbitmq | files-only (mnesia dir + message store) | data -> /var/lib/rabbitmq | restoreMount | server boot, `await_startup`, `check_running`, `check_local_alarms`, canary queue message count | The node name is tied to mnesia (`RABBITMQ_NODENAME`). `.erlang.cookie` must survive the restore with mode 400. |
| nextcloud | database + files | html -> /var/www/html; pgdata -> /var/lib/postgresql/data | restoredVolumes + native sidecar postgres | `occ status` (DB reachable), `occ integrity:check-core`, canary: `oc_filecache.size` equals the file size on disk | pdo_pgsql in the image is assumed. The canary path assumes one storage. `dbhost` is rewritten to localhost in the config. |
| wordpress | database + files | html -> /var/www/html; mysql -> /var/lib/mysql | restoredVolumes + native sidecar mariadb | mariadb ping, canary post content == sha256 of uploaded file | The front-end render is not checked. Plugin-generated tables are not covered. |
| gitea | sqlite-in-volume (gitea.db + git repos) | data -> /data | restoreMount (/data) | `gitea web` boot, `/api/healthz` pass, canary repo raw file sha256 | Only SQLite is covered. Postgres-backed installs need a separate DB restore, which is not covered. The repo and DB share one PVC, so one snapshot covers both. |
| grafana | sqlite-in-volume (grafana.db) | data -> /var/lib/grafana | restoreMount | server boot, `/api/health` database ok, canary dashboard via admin API | The admin password must match the source. Provisioning files from the ConfigMap are not included. |
| prometheus | files-only (TSDB blocks + WAL) | data -> /prometheus | restoreMount | `promtool tsdb list`, server ready, `max_over_time(bret_canary_seq[3650d])` equals seeded value | The canary metric must be ingested before the backup. Watch WAL replay time on large data. |
| vaultwarden | sqlite-in-volume (db.sqlite3, attachments, icons) | data -> /data | restoreMount | python `integrity_check` (read-only), canary user row, app boots and `/alive` 200 | The tag was pulled OK in the test. Attachments are large. Migrations run on a restored DB. |
| home-assistant | sqlite-in-volume (home-assistant_v2.db) + .storage files | config -> /config | restoreMount | recorder DB integrity (read-only), canary file sha256, HTTP 401 from `/api/` | Start can exceed the time budget on slow nodes. The recorder DB can be large. |
| jellyfin | sqlite-in-volume (library.db, jellyfin.db) + media files | config -> /config; media -> /media | restoredVolumes | `/health` Healthy, canary media file sha256 | The media PVC can be very large. Option: restore the config only. The image has no sqlite3, so the DB integrity is not checked. |
| uptime-kuma | sqlite-in-volume (kuma.db) | data -> /app/data | restoreMount | read-only `integrity_check` and canary monitor row via the app's own sqlite3 module, server boot, entry-page | The schema is for 1.x only. 2.x differs. UID 1000 is assumed. The entry-page grep is weak. |
| keycloak | database-only (external PostgreSQL) | pgdata -> /var/lib/postgresql/data (sidecar) | restoreMount + native sidecar postgres | `kc.sh start` against restored DB (Liquibase), `/health/ready` UP, canary realm OIDC discovery | The tag was pulled OK in the test. Start can be slow. There is no separate realm export. |
| n8n | sqlite-in-volume (database.sqlite) | data -> /home/node/.n8n | restoreMount | decrypted credential and workflow export contain the canary (proves `N8N_ENCRYPTION_KEY`), app `/healthz` ok | The encryption key is in a Secret, not in the volume. Postgres installs are not covered. |
| pihole | sqlite-in-volume (gravity.db) + config files | etc-pihole -> /etc/pihole; dnsmasq -> /etc/dnsmasq.d | restoredVolumes | gravity.db integrity and canary adlist, canary DNS line in custom.list, `pihole-FTL dnsmasq-test` | The `dnsmasq-test` subcommand is not confirmed in 2024.07. The FTL DB schema is tied to the image version. |

Tag status: these tags were probed OK on Docker Hub or quay:
postgres:16.4, mysql:8.4, mariadb:11.4, redis:7.4,
mongo:7.0, rabbitmq:3.13-management, nextcloud:29-apache, wordpress:6.6-php8.3-apache,
gitea/gitea:1.22, grafana/grafana:11.2.0, vaultwarden/server:1.32.0,
homeassistant/home-assistant:2024.9.0, jellyfin/jellyfin:10.9.11, louislam/uptime-kuma:1.23.15,
n8nio/n8n:1.60.0, pihole/pihole:2024.07.0, prom/prometheus:v2.54.1.
Not confirmed: elasticsearch, python, keycloak. Each is marked in the YAML.

Popularity: the popularity list was used as given. No apps were swapped. Artifact Hub and CNCF
usage were not checked.

## Apps that have data and a database

These apps keep state in two places: files and a DB, or a DB with a separate file store. Both
must come from the same backup point.

- **nextcloud** (files + postgres, two PVCs, restoredVolumes). `/var/www/html/data` holds the
  files. The `oc_filecache` table in postgres holds the rows that list the files and their sizes.
  If the DB comes from 10:00 and the files come from 10:05, rows point at missing files, or files have
  no rows. Symptoms: broken file lists and "file not found" in the UI. `occ files:scan` can repair
  listings. It may drop or duplicate entries. Handling: restore both PVCs from one backup selector in
  one attempt. Check: the canary `oc_filecache.size` must equal the file size on disk.
- **wordpress** (files + mariadb, two PVCs, restoredVolumes). `wp_posts` rows reference uploads in
  `wp-content/uploads`. A DB newer than the files causes broken images. A DB older than the files
  causes orphan uploads. Handling: same as nextcloud. Use one attempt and both volumes. Check: the
  canary post content must equal the sha256 of the uploaded canary file.
- **jellyfin** (config sqlite + media files, two PVCs, restoredVolumes). `library.db` stores the media
  item paths and IDs. If the media comes from a different point, items show as missing or unavailable,
  and the metadata is wrong. Handling: restore config and media in one attempt. Mount media at its
  original path. Check: the canary media file hash and `/health`. Option: restore the config only if
  the media is too large. In that case, the check covers the DB only.
- **pihole** (gravity.db + dnsmasq.d, two PVCs, restoredVolumes). Adlists and custom DNS live in
  `gravity.db` and `custom.list`. The config files in `/etc/dnsmasq.d` must match the FTL version. A
  mix causes lists that are in the DB but not applied. It can also cause missing DNS records. Check:
  the canary adlist row in gravity.db, the canary line in custom.list, and that the dnsmasq config
  parses.
- **keycloak** (postgres only, one PVC, native sidecar). This check has no file store. If themes or
  providers live on a second PVC, they must be restored with the DB. Add restoredVolumes. Check: the
  realm OIDC discovery and `/health/ready`.
- **n8n** (sqlite, one PVC, encryption key in a Secret). The DB stores credentials encrypted with
  `N8N_ENCRYPTION_KEY`. A DB restored with a different key cannot decrypt the credentials. Workflows
  break silently. Check: the decrypted export must contain the canary credential. A failure here means
  a key mismatch, not a bad backup.
- **gitea, grafana, home-assistant, vaultwarden, uptime-kuma** (sqlite in one PVC). The DB and files
  share one PVC. One snapshot covers both. The risk is the SQLite file itself. The backup must be taken
  with the app stopped, or from a consistent snapshot. Checks: DB integrity, and the canary row or file.
- **postgres, mysql, mariadb, redis, mongodb** (database only). The check reads the DB's own structure
  (`pg_amcheck`, `mysqlcheck`, `validate`) and the canary row. A physical copy taken while the DB runs
  is recovered by crash recovery. That recovery is part of the test.
- **elasticsearch, prometheus, rabbitmq** (files only, one PVC). Each has its own on-disk format. The
  checks rely on the server to load the restored data and answer a query for the canary.
