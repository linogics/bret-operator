<h1 align="center">bret-operator</h1>
<p align="center"><img align="center" src="docs/bret.png" alt="bret" /></p>
<p align="center">
  <a href="https://github.com/linogics/bret-operator/actions/workflows/test.yml"><img src="https://github.com/linogics/bret-operator/actions/workflows/test.yml/badge.svg" alt="Tests"></a>
  <a href="https://github.com/linogics/bret-operator/actions/workflows/lint.yml"><img src="https://github.com/linogics/bret-operator/actions/workflows/lint.yml/badge.svg" alt="Lint"></a>
  <a href="https://github.com/linogics/bret-operator/releases"><img src="https://img.shields.io/github/v/release/linogics/bret-operator?include_prereleases&sort=semver" alt="Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/linogics/bret-operator" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-green" alt="License: BSD-3-Clause"></a>
</p>

**bret** stands for **b**ackup **re**store **T**est.

bret tests your Kubernetes backups. It restores a backup from Velero, VolSync or
Kopiur into a scratch volume. It runs your check in a throwaway pod. Then it records the result. An app with several volumes, such as nextcloud with `html` and
`pgdata`, is restored and checked as one unit.

## Quick links

- [Getting started](docs/getting-started.md): from an empty cluster to a first check.
- [Installation](docs/installation.md): Helm chart or plain manifests.
- [Backup providers](docs/providers/README.md): Kopiur, VolSync and Velero, each with a tested
  WordPress + MariaDB example.
- [Concepts](docs/concepts.md): what the operator does and the rules it follows.
- [Checks](docs/checks.md): how to write a check. App drafts are in
  [checks/](checks/INDEX.md).
- [API reference](docs/reference/api.md): generated from the Go types.

## Install

```bash
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --version 0.1.0 --namespace bret-operator-system --create-namespace
```

## Develop

```bash
make test lint          # unit tests and linter
make docs helm-sync     # regenerate docs, chart CRDs and RBAC
```

See [docs/development.md](docs/development.md) and [docs/releasing.md](docs/releasing.md).

## License

BSD-3-Clause. See [LICENSE](LICENSE).
