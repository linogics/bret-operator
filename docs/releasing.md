# Releasing

A release is a git tag. Pushing a tag that matches `v*.*.*` runs `.github/workflows/release.yml`.
It publishes three things, all for the same version.

| Artifact | Where |
|---|---|
| Container image, linux amd64 and arm64 | `ghcr.io/linogics/bret-operator:<version>` |
| Helm chart | `oci://ghcr.io/linogics/charts/bret-operator:<version>` |
| Plain manifests `install.yaml` and the chart `.tgz` | GitHub release `v<version>` |

## Before you tag

1. `main` is green: the Tests, Lint, Docs and Helm chart workflows pass.
2. Generated files are current. `make docs-check` and `make helm-lint` pass locally.
3. CRD changes are reviewed. Helm does not upgrade CRDs. Note any schema change in the release
   notes, and tell users to apply the CRDs first (see [installation.md](installation.md#upgrading)).
4. The version you tag is a valid semantic version. Use `-rc.1` suffixes for pre-releases. The
   workflow marks those as pre-releases on GitHub.
5. The blueprint catalog status in [blueprints/INDEX.md](../blueprints/INDEX.md) matches what you
   claim in the release notes.

## Cut the release

```bash
git switch main
git pull --ff-only
git tag -a v0.1.0 -m "release v0.1.0"
git push origin v0.1.0
```

The version in the tag is used for the image tag, the chart version, the chart `appVersion`
and the GitHub release name. The chart's `Chart.yaml` keeps its own `0.1.0` in git. The release
job overrides it at package time.

## What the workflow does

1. Runs `make test`. A failing test stops the release.
2. Builds and pushes the multi-architecture image with the tag and the source labels.
3. Runs `make build-installer` with that image, producing `dist/install.yaml`.
4. Runs `make helm-package VERSION=<version>`, which lints the chart and writes
   `dist/bret-operator-<version>.tgz`.
5. Pushes the chart to the OCI registry.
6. Creates the GitHub release with `install.yaml` and the chart attached, and generated notes.

## Check the release

```bash
docker buildx imagetools inspect ghcr.io/linogics/bret-operator:0.1.0
helm pull oci://ghcr.io/linogics/charts/bret-operator --version 0.1.0
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator --version 0.1.0 \
  --namespace bret-operator-system --create-namespace --dry-run
```

## Fix a bad release

Do not move a tag that was already published. Publish a new patch version instead, and mark the
bad release as a pre-release or delete its GitHub release. Deleting a GHCR image tag is possible
but breaks anyone who already pulled it.

## Secrets and permissions

The workflow uses `GITHUB_TOKEN` with `contents: write` and `packages: write`. It needs no other
secret. The repository must allow Actions to write packages.
