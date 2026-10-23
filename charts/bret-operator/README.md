# bret-operator

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

Kubernetes operator that tests backups by restoring them from Velero, VolSync or Kopiur and running a check against the restored data.

**Homepage:** <https://github.com/linogics/bret-operator>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Tobias M. |  | <https://github.com/linogics> |

## Source Code

* <https://github.com/linogics/bret-operator>

## Requirements

Kubernetes: `>=1.29.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity for the manager pod. |
| commonLabels | object | `{}` | Extra labels added to all resources and pods. |
| extraArgs | list | `[]` | Extra command line arguments for the manager binary, for example `--zap-log-level=debug`. |
| extraEnv | list | `[]` | Extra environment variables for the manager container. |
| fullnameOverride | string | `""` | Override the full release name. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy. |
| image.repository | string | `"ghcr.io/linogics/bret-operator"` | Container image repository. |
| image.tag | string | `""` | Image tag. Defaults to the chart appVersion when empty. |
| imagePullSecrets | list | `[]` | Image pull secrets for private registries. |
| metrics.enabled | bool | `true` | Serve Prometheus metrics. When false the metrics endpoint is disabled. |
| metrics.port | int | `8443` | Port of the metrics endpoint. |
| metrics.secure | bool | `true` | Serve metrics over HTTPS with token authentication and authorization. |
| nameOverride | string | `""` | Override the chart name. |
| nodeSelector | object | `{}` | Node selector for the manager pod. |
| podAnnotations | object | `{}` | Extra annotations added to the manager pod. |
| podLabels | object | `{}` | Extra labels added to the manager pod. |
| podSecurityContext | object | `{"runAsNonRoot":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Pod security context. The default follows the restricted Pod Security Standard. |
| replicaCount | int | `1` | Number of manager replicas. Leader election keeps one replica active. |
| resources | object | `{"limits":{"cpu":"500m","memory":"128Mi"},"requests":{"cpu":"10m","memory":"64Mi"}}` | Manager resources. |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}` | Container security context. |
| serviceAccount.annotations | object | `{}` | Annotations added to the service account. |
| serviceAccount.create | bool | `true` | Create the service account used by the manager. |
| serviceAccount.name | string | `""` | Name of the service account. Generated from the release name when empty. |
| tolerations | list | `[]` | Tolerations for the manager pod. |

