# Development

## Prerequisites

| Tool | Version | Used for |
|---|---|---|
| Go | 1.24 or newer (see `go.mod`) | build, tests |
| kubectl | matching your cluster | cluster work |
| Helm | 3.22.1 (CI pins this) | chart lint and package |
| Docker or nerdctl | any | image build, kind |
| kind | v0.33 or newer | local clusters |
| Python 3 with PyYAML | 3.10 or newer | blueprint harness |

The Makefile downloads its own copies of controller-gen, setup-envtest, golangci-lint,
kustomize, crd-ref-docs and helm-docs into `bin/`.

## Build, test, lint

```bash
make build           # manifests, generate, fmt, vet, then bin/manager
make test            # unit tests and envtest; writes cover.out
make lint            # golangci-lint v2.15.0 with the repo's .golangci.yml
make lint-fix        # same, with auto-fixes
```

After you change a type in `api/v1alpha1/`:

```bash
make manifests generate   # CRDs in config/crd/bases, deepcopy functions
make docs                 # API reference and chart README
make helm-sync            # copy CRDs and RBAC into charts/bret-operator
```

CI runs `make docs-check` and `make helm-lint`. Both fail when generated files are out of date.
Commit the regenerated files with your change.

## Local cluster

The spike ran on kind with the nerdctl provider. kind uses nerdctl when it is on the path and
containerd is running.

```bash
export KIND_EXPERIMENTAL_PROVIDER=nerdctl
kind create cluster --name bret-spike --image kindest/node:v1.37.0
```

Install the CSI hostpath driver for snapshots. The spike used csi-driver-host-path v1.19.0 with
external-snapshotter v8.7.0. Create a `VolumeSnapshotClass` named `csi-hostpath-snapclass` and a
`StorageClass` named `csi-hostpath-sc`.

Install the backup tools you test with. See [backup-providers.md](backup-providers.md). The
blueprint harness expects an S3-compatible endpoint named `versitygw.storage.svc.cluster.local:7070`
with buckets `kopiur`, `velero` and `volsync`. The spike used Versity Gateway with the posix
backend for this.

Install the operator's CRDs and run the manager against the cluster:

```bash
make install          # CRDs only
make run              # manager on your machine, using your kubeconfig
```

Delete the cluster when done:

```bash
kind delete cluster --name bret-spike
```

## Blueprint tests

Blueprint tests need a cluster with Kopiur and the S3 endpoint above. Read
[testing-blueprints.md](testing-blueprints.md). Run one app:

```bash
TIMEOUT=900 bash hack/blueprint-test/roundtrip.sh postgres
```

Blueprint tests do not run in CI. They need a cluster and several gigabytes of images.

## Layout

| Path | Contents |
|---|---|
| `api/v1alpha1/` | API types. The source for CRDs and the reference. |
| `internal/controller/` | Reconcilers. Empty scaffolds at this stage. |
| `cmd/main.go` | Manager entry point and flags. |
| `config/` | Kustomize bases: CRDs, RBAC, manager, metrics. |
| `charts/bret-operator/` | Helm chart. CRDs and RBAC are copied from `config/`. |
| `blueprints/` | Check drafts for 20 apps. See `blueprints/INDEX.md`. |
| `examples/` | Minimal blueprint, RestoreTest and Restore examples. |
| `hack/blueprint-test/` | Blueprint harness and per-app seeds. |
| `hack/helm/sync_rbac.py` | Copies RBAC rules into the chart. |
| `docs/` | This documentation. `docs/reference/api.md` is generated. |
| `docs/superpowers/specs/` | Design document. |

## Conventions

- Conventional commits, short subject, no co-author trailers.
- Code and comments in English.
- Shell scripts: tabs, `[[ ]]`, no `set -e`, quoted expansions.
- Keep generated files in git, and keep them in sync with CI (`make docs-check`, `make helm-sync`).
