<h1 align="center">bret-operator</h1>
<p align="center"><img align="center" src="docs/bret.png" alt="bret" /></p>
<p align="center">
  <a href="https://github.com/linogics/bret-operator/actions/workflows/test.yml"><img src="https://github.com/linogics/bret-operator/actions/workflows/test.yml/badge.svg" alt="Tests"></a>
  <a href="https://github.com/linogics/bret-operator/actions/workflows/lint.yml"><img src="https://github.com/linogics/bret-operator/actions/workflows/lint.yml/badge.svg" alt="Lint"></a>
  <a href="https://github.com/linogics/bret-operator/releases"><img src="https://img.shields.io/github/v/release/linogics/bret-operator?include_prereleases&sort=semver" alt="Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/linogics/bret-operator" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-green" alt="License: BSD-3-Clause"></a>
</p>

***bret*** stands for ***b***ackup ***re***store ***t***est.

bret tests your Kubernetes backups. It restores a backup from Velero, VolSync or
Kopiur into a scratch volume. It runs your check in a throwaway pod. Then it records the result. An app with several volumes, such as wordpress with `html` and
`mysql`, is restored and checked as one unit.

## Documentation

Everything is available on **[bret-operator.linogics.io](https://bret-operator.linogics.io/)**:

- [Quickstart](https://bret-operator.linogics.io/start/quickstart/): from an empty cluster to a first check.
- [Backup providers](https://bret-operator.linogics.io/providers/): Velero, VolSync and Kopiur compared, with a checklist for
  an existing setup.
- [Check catalog](https://bret-operator.linogics.io/guides/check-catalog/): ready-made checks for several apps.
- [API reference](https://bret-operator.linogics.io/reference/api/): generated from the Go types.

## Install

```bash
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --version 0.1.0 --namespace bret-operator-system --create-namespace
```

## Develop

See [Development](https://bret-operator.linogics.io/contributing/development/) and [Releasing](https://bret-operator.linogics.io/contributing/releasing/).

## License

BSD-3-Clause. See [LICENSE](LICENSE).
