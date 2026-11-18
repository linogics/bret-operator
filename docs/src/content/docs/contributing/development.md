---
title: Development
description: Build, test, lint, generate docs and the local kind setup.
---

## Prerequisites

| Tool | Version | Used for |
|---|---|---|
| Go | 1.27.4 or newer (see `go.mod`) | build, tests |
| kubectl | matching your cluster | cluster work |
| Helm | 3.22.1 (CI pins this) | chart lint and package |
| Docker or nerdctl | any | image build, kind |
| kind | v0.33 or newer | local clusters |
| Python 3 with PyYAML | 3.10 or newer | check harness |

The Makefile downloads its own copies of controller-gen, setup-envtest, golangci-lint,
kustomize, crd-ref-docs and helm-docs into `bin/`.

## Build, test, lint

```bash
make build           # manifests, generate, fmt, vet, then bin/manager
make test            # unit tests and envtest; writes cover.out
make lint            # golangci-lint v2.15.0 with the repo's .golangci.yml
make lint-fix        # same, with auto-fixes
make yamllint        # strict yamllint on tracked YAML; flow lists are forbidden, see .yamllint.yaml
```

After you change a type in `api/v1alpha1/`:

```bash
make manifests generate   # CRDs in config/crd/bases, deepcopy functions
make docs                 # API reference and chart README
make helm-sync            # copy CRDs into charts/bret-operator-crds, RBAC into charts/bret-operator
```

CI runs `make docs-check` and `make helm-lint`. Both fail when generated files are out of date. CI also runs `make yamllint`.
Commit the regenerated files with your change.

## Local cluster

kind uses the nerdctl provider when nerdctl is on the path and containerd is running.

```bash
export KIND_EXPERIMENTAL_PROVIDER=nerdctl
kind create cluster --name bret-dev --image kindest/node:v1.37.0
```

Install the CSI hostpath driver for snapshots. Tested with csi-driver-host-path v1.19.0 and
external-snapshotter v8.7.0. Create a `VolumeSnapshotClass` named `csi-hostpath-snapclass` and a
`StorageClass` named `csi-hostpath-sc`.

Install the backup tools you test with. See [providers/](/providers/). The
check harness expects an S3-compatible endpoint named `versitygw.storage.svc.cluster.local:7070`
with buckets `kopiur`, `velero` and `volsync`. Versity Gateway with the posix backend works for this.

Install the operator's CRDs, and run the manager against the cluster:

```bash
make install          # CRDs only
make run              # manager on your machine, using your kubeconfig
```

Delete the cluster when you finish:

```bash
kind delete cluster --name bret-dev
```

## Check tests

Check tests need a cluster with Kopiur and the S3 endpoint above. Read
[testing-checks.md](/guides/test-a-check/). Run one app:

```bash
TIMEOUT=900 bash hack/check-test/roundtrip.sh postgres
```

Check tests do not run in CI. They need a cluster and several gigabytes of images.

## Layout

| Path | Contents |
|---|---|
| `api/v1alpha1/` | API types. The source for CRDs and the reference. |
| `internal/controller/` | Reconcilers for Restore, RestoreCheck and RestoreTest. |
| `cmd/main.go` | Manager entry point and flags. |
| `config/` | Kustomize bases: CRDs, RBAC, manager, metrics. |
| `charts/bret-operator/` | Helm chart. RBAC is copied from `config/`. Depends on `charts/bret-operator-crds/`, which holds the CRDs. |
| `charts/bret-operator-crds/` | Helm chart with the CRDs only. Copied from `config/crd/bases/`. |
| `checks/` | One folder per app: check, seed and README. See `checks/README.md`. |
| `examples/` | WordPress + MariaDB (`wordpress/`) and per tool (`kopiur/`, `volsync/`, `velero/`) the backup setup, a `Restore`, a `RestoreTest` and a negative `Restore`. |
| `hack/check-test/` | Check harness. The seeds live in `checks/<app>/seed.yaml`. |
| `hack/helm/sync_rbac.py` | Copies RBAC rules into the chart. |
| `docs/` | This documentation. `docs/reference/api.md` is generated. |

## Conventions

- Use conventional commits. Keep the subject short. Do not add co-author trailers.
- Write code and comments in English.
- Shell scripts: use tabs, `[[ ]]`, no `set -e`, and quoted expansions.
- Keep generated files in git. Keep them in sync with CI (`make docs-check`, `make helm-sync`).
