---
title: Releasing
description: How to cut a release and what it publishes.
---

A release is a git tag. Pushing a tag that matches `v*.*.*` runs `.github/workflows/release.yml`.
The workflow publishes three kinds of artifact. All of them have the same version.

| Artifact | Where |
|---|---|
| Container image, linux amd64 and arm64 | `ghcr.io/linogics/bret-operator` and the Docker Hub mirror `docker.io/linogics/bret-operator` |
| Helm charts `bret-operator` and `bret-operator-crds` | `oci://ghcr.io/linogics/charts/<chart>:<version>` |
| Plain manifests `install.yaml` and both chart `.tgz` files | GitHub release `v<version>` |

Both registries get the same image digest and tags: `<version>`, `<major>.<minor>`, `<major>` and
`latest`. Pre-releases get `<version>` only. Version 0.x releases get no `<major>` tag. Charts and
manifests point at GHCR. Docker Hub is a mirror and holds no charts, because a chart named
`bret-operator` would share the image repository there.

The workflow needs the repository secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` (a Docker Hub
personal access token with Read & Write scope). Docker Hub tokens can expire. An expired token fails
the whole release, so renew it before it runs out.

## Before you tag

1. `main` is green. The Tests, Lint, Docs and Helm chart workflows pass.
2. Generated files are current. `make docs-check` and `make helm-lint` pass locally.
3. Review CRD changes. Helm does not upgrade CRDs. Note any schema change in the release
   notes. Tell users to apply the CRDs first (see [installation.md](/start/installation/#upgrading)).
4. The version you tag is a valid semantic version. Use `-rc.1` suffixes for pre-releases. The
   workflow marks those as pre-releases on GitHub.
5. The check catalog status in [checks/README.md](/guides/check-catalog/) matches what you
   claim in the release notes.

## Cut the release

```bash
git switch main
git pull --ff-only
git tag -a v0.1.0 -m "release v0.1.0"
git push origin v0.1.0
```

The version in the tag is used for the image tag, the chart version, the chart `appVersion`
and the GitHub release title. The charts' `Chart.yaml` keeps its own `0.1.0` in git. The release
job overrides it at package time.

## What the workflow does

1. Runs `make test`. A failing test stops the release.
2. Builds and pushes the multi-architecture image with the tag and the source labels.
3. Runs `make build-installer` with that image. This produces `dist/install.yaml`.
4. Runs `make helm-package VERSION=<version>`. This lints both charts and writes
   `dist/bret-operator-<version>.tgz` and `dist/bret-operator-crds-<version>.tgz`.
5. Pushes both charts to the OCI registry. The CRD chart is pushed first.
6. Creates the GitHub release with `install.yaml` and both chart `.tgz` files attached, and
   generated notes.

## Check the release

```bash
docker buildx imagetools inspect ghcr.io/linogics/bret-operator:0.1.0
helm pull oci://ghcr.io/linogics/charts/bret-operator --version 0.1.0
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator --version 0.1.0 \
  --namespace bret-operator-system --create-namespace --dry-run
```

## Fix a bad release

Do not move a tag that was already published. Publish a new patch version instead. Mark the
bad release as a pre-release, or delete its GitHub release. Deleting a GHCR image tag is possible,
but it breaks anyone who already pulled the image.

## Secrets and permissions

The workflow uses `GITHUB_TOKEN` with `contents: write` and `packages: write`. It needs no other
secret. The repository must allow Actions to write packages.
