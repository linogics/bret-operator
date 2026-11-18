# Installation

This page installs the bret CRDs and the manager. Install a backup tool first. See
[providers/](providers/README.md).

## Requirements

| Requirement | Version | Why |
|---|---|---|
| Kubernetes | 1.29 or newer | Native sidecars in checks (`initContainers` with `restartPolicy: Always`). |
| Helm | 3.14 or newer (the release is tested with 3.22.1) | Installs the chart. |
| CSI driver with snapshot support | any | `copyMethod: Snapshot` needs a `VolumeSnapshotClass`. |
| Backup tool | Velero 1.18, VolSync 0.17, or Kopiur 0.10 | The operator drives its restore objects. |

## Verified versions

| Component | Version |
|---|---|
| Velero | v1.18.4 |
| Velero AWS plugin | v1.14.4 |
| VolSync | 0.17.0 |
| Kopiur | 0.10.21 |
| PostgreSQL | 17.12 |
| kind node image | v1.37.0 |
| csi-driver-host-path | v1.19.0 |
| external-snapshotter | v8.7.0 |

## Option A: Helm chart (recommended)

The chart is published as an OCI artifact with each release:

```bash
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --version 0.1.0 \
  --namespace bret-operator-system \
  --create-namespace
```

Check the installation:

```bash
kubectl -n bret-operator-system get deploy,pods
kubectl -n bret-operator-system logs deploy/bret-operator
kubectl get crd | grep bret.linogics.io
```

Expected: the deployment is `1/1 Available`, and three CRDs are listed:
`restorechecks`, `restores`, `restoretests`.

### Values you will likely change

The full table is in [charts/bret-operator/README.md](../charts/bret-operator/README.md).

| Value | Default | Change it when |
|---|---|---|
| `image.repository` | `ghcr.io/linogics/bret-operator` | You pull from the Docker Hub mirror `docker.io/linogics/bret-operator`. |
| `image.tag` | chart `appVersion` | You pin a different image. |
| `replicaCount` | `1` | Leader election keeps one active replica. More replicas only add standby. |
| `metrics.enabled` | `true` | You do not scrape Prometheus. |
| `metrics.secure` | `true` | Keep it `true`. Scrapers need a token (see below). |
| `resources` | requests 10m CPU / 64Mi memory; limits 500m CPU / 128Mi memory | Raise the memory limit when the cache grows with many objects. |
| `extraArgs` | `[]` | You want more manager flags, for example `--zap-log-level=debug`. |
| `pollInterval` | `30s` | A running attempt should be checked more or less often. Minimum `5s`. Each poll is one API read per running attempt. |

Example values file:

```yaml
# restore-values.yaml
metrics:
  enabled: true
  secure: true
resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 10m
    memory: 128Mi
extraArgs:
  - --zap-log-level=info
```

```bash
helm upgrade --install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --version 0.1.0 -n bret-operator-system -f restore-values.yaml
```

### Metrics scraping

With `metrics.secure: true`, the endpoint on port 8443 needs a bearer token. The token's
service account needs `get` on the `/metrics` non-resource URL. Example:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: bret-operator-metrics-reader
rules:
  - nonResourceURLs: ["/metrics"]
    verbs: ["get"]
```

Bind it to your Prometheus service account with a `ClusterRoleBinding`.

## Option B: plain manifests

Each release attaches `install.yaml` to its GitHub release. It has the CRDs, RBAC, and the
deployment, all in `bret-operator-system`:

```bash
kubectl apply -f https://github.com/linogics/bret-operator/releases/download/v0.1.0/install.yaml
```

Plain manifests do not upgrade cleanly across versions. Prefer the Helm chart for upgrades.

## Upgrading

The CRDs ship in the `bret-operator-crds` chart, which the `bret-operator` chart depends on. Helm
installs CRDs once and does not update them on upgrade. A new release can change the CRD schema.
So apply the CRDs yourself before you upgrade the chart:

```text
apply CRDs (server-side) ─► helm upgrade
```

```bash
kubectl apply --server-side -f https://github.com/linogics/bret-operator/releases/download/v0.1.0/install.yaml
helm upgrade bret-operator oci://ghcr.io/linogics/charts/bret-operator --version 0.2.0 -n bret-operator-system
```

Server-side apply avoids the size limit on the `last-applied` annotation for large CRDs.

### Upgrading to multi-volume sources

- Recreate every existing `Restore`, `RestoreTest` and `RestoreCheck` in the new shape. Old
  objects have no `sources`: a `Restore` ends `Failed` with `SourceMissing`, and a `RestoreTest`
  blocks and starts no runs.
- The keys of the `bret.linogics.io/mover-uid` annotation are now `sourcePVC` values. Old keys
  are ignored without an error, and the mover falls back to the `runAsUser` of the container.
- `RestoreTest` `afterBackup` watermarks reset once. They moved to `status.lastHandledBackups`.

### Upgrading to RestoreTest for all providers

- A Velero `Restore` source needs `sourcePVC`, like the other providers. List one source per
  PVC, all with the same `backupRef`.
- The Velero selectors moved from `sources[].labelSelector` and `sources[].orLabelSelectors` to
  `spec.velero.labelSelector` and `spec.velero.orLabelSelectors`.
- `RestoreTest` `status.pendingBackups` changed from a map to a list of
  `{sourcePVC, name, createdAt}` entries, like `lastHandledBackups`. The operator cannot read a
  `RestoreTest` that still holds the old map, and logs an error for it until you recreate it.
  Recreate every `RestoreTest` that has a queued run.
- A `backupSelector.name` must now name a finished backup. Before, the name was used as is, even
  for a Kopiur `Snapshot` that had not succeeded. Now the test waits with `BackupNotFound`.

## Uninstalling

```bash
helm uninstall bret-operator -n bret-operator-system
```

Helm leaves the CRDs and any custom resources in place. Remove them only if you no longer
need your `Restore`, `RestoreTest` and `RestoreCheck` objects:

```bash
kubectl delete restoretests,restores,restorechecks --all -A
kubectl delete crd restores.bret.linogics.io restoretests.bret.linogics.io restorechecks.bret.linogics.io
```

Deleting a CRD deletes every object of that kind. Back up the objects first if you keep them.

## Where things live

| Object | Name | Namespace |
|---|---|---|
| Deployment | `bret-operator` | `bret-operator-system` |
| ServiceAccount | `bret-operator` | `bret-operator-system` |
| Metrics Service | `bret-operator-metrics` | `bret-operator-system` |
| Leader election Role | `bret-operator-leader-election` | `bret-operator-system` |
| ClusterRole | `bret-operator-manager`, `bret-operator-metrics-auth` | cluster |

The names change with the release name. The Helm names above assume `helm install bret-operator`.
