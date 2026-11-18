<p align="center"><img src="https://raw.githubusercontent.com/linogics/bret-operator/main/docs/bret.png" alt="bret" width="300" /></p>

# bret-operator

***bret*** stands for ***b***ackup ***re***store ***t***est.

A Kubernetes operator that tests your backups. It restores a Velero, VolSync or Kopiur
backup into a scratch volume, runs your check in a throwaway pod and records the result.

Source, docs and issues: https://github.com/linogics/bret-operator

## Tags

| Tag      | Example | Moves with                    |
|----------|---------|-------------------------------|
| `X.Y.Z`  | `0.1.0` | never, pinned release          |
| `X.Y`    | `0.1`   | latest patch of that minor     |
| `X`      | `0`     | latest minor of that major     |
| `latest` |         | latest stable release          |

Pre-releases such as `0.2.0-rc.1` only get their full version tag.

Platforms: `linux/amd64`, `linux/arm64`.

The same image is published to `ghcr.io/linogics/bret-operator`.

## Install

The Helm chart is the supported way to run the operator.

```bash
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --namespace bret-operator-system --create-namespace \
  --set image.repository=docker.io/linogics/bret-operator
```

Plain manifests are attached to each
[GitHub release](https://github.com/linogics/bret-operator/releases).

## Docs

- [Documentation](https://bret-operator.linogics.io/)
- [Quickstart](https://bret-operator.linogics.io/start/quickstart/)
- [Installation](https://bret-operator.linogics.io/start/installation/)
- [Backup providers](https://bret-operator.linogics.io/providers/)
- [Write a check](https://bret-operator.linogics.io/guides/write-a-check/)

## License

BSD-3-Clause.
