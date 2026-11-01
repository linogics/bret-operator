# Installation

This page installs the bret-operator CRDs and the manager. Install a backup tool first, see
[backup-providers.md](backup-providers.md). The operator does not run restores until its
controllers exist (see the status table in [README.md](README.md)).

## Requirements

| Requirement | Version | Why |
|---|---|---|
| Kubernetes | 1.29 or newer | Native sidecars in blueprints (`initContainers` with `restartPolicy: Always`). |
| Helm | 3.14 or newer (the release is tested with 3.22.1) | Installs the chart. |
| CSI driver with snapshot support | any | `copyMethod: Snapshot` needs a `VolumeSnapshotClass`. |
| Backup tool | Velero 1.18, VolSync 0.17, or Kopiur 0.10 (alpha) | The operator drives its restore objects. |

Verified versions are listed in [spike-findings.md](spike-findings.md).

## Option A: Helm chart (recommended)

The chart is published as an OCI artifact with each release:

```bash
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --version 0.1.0 \
  --namespace bret-operator-system \
  --create-namespace
```

To install from a checkout of this repository:

```bash
make helm-sync                       # copies CRDs and RBAC from config/ into the chart
helm install bret-operator ./charts/bret-operator \
  --namespace bret-operator-system \
  --create-namespace
```

Check it:

```bash
kubectl -n bret-operator-system get deploy,pods
kubectl -n bret-operator-system logs deploy/bret-operator
kubectl get crd | grep bret.linogics.io
```

Expected: the deployment is `1/1 Available`, and three CRDs are listed:
`restoreblueprints`, `restores`, `restoretests`.

### Values you will likely change

The full table is in [charts/bret-operator/README.md](../charts/bret-operator/README.md).

| Value | Default | Change it when |
|---|---|---|
| `image.tag` | chart `appVersion` | You pin a different image. |
| `replicaCount` | `1` | Leader election keeps one active replica. More replicas only add standby. |
| `metrics.enabled` | `true` | You do not scrape Prometheus. |
| `metrics.secure` | `true` | Keep it `true`. Scrapers need a token (see below). |
| `resources` | 10m/64Mi request, 128Mi limit | Raise the memory limit when the cache grows with many objects. |
| `extraArgs` | `[]` | You want more manager flags, for example `--zap-log-level=debug`. |

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

With `metrics.secure: true` the endpoint on port 8443 needs a bearer token. The token's
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

Build it yourself from a checkout:

```bash
make build-installer IMG=ghcr.io/linogics/bret-operator:0.1.0
kubectl apply -f dist/install.yaml
```

Plain manifests do not upgrade cleanly across versions. Prefer the Helm chart for upgrades.

## Upgrading

Helm installs CRDs from `crds/` once. It does not update them on upgrade. A new release can
change the CRD schema, so apply the CRDs yourself before you upgrade the chart:

```bash
kubectl apply --server-side -f https://github.com/linogics/bret-operator/releases/download/v0.1.0/install.yaml
# or, from a checkout:
kubectl apply --server-side -f charts/bret-operator/crds/
helm upgrade bret-operator oci://ghcr.io/linogics/charts/bret-operator --version 0.2.0 -n bret-operator-system
```

Server-side apply avoids the size limit on the `last-applied` annotation for large CRDs.

## Uninstalling

```bash
helm uninstall bret-operator -n bret-operator-system
```

Helm leaves the CRDs and any custom resources in place. Remove them only if you no longer
need your `Restore`, `RestoreTest` and `RestoreBlueprint` objects:

```bash
kubectl delete restoretests,restores,restoreblueprints --all -A
kubectl delete crd restores.bret.linogics.io restoretests.bret.linogics.io restoreblueprints.bret.linogics.io
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

Names change with the release name. The Helm names above assume `helm install bret-operator`.
