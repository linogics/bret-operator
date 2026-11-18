{{- define "gvList" -}}
{{- $groupVersions := . -}}
---
title: API reference
description: Generated field reference for RestoreCheck, Restore and RestoreTest.
---

Generated from the Go types in `api/v1alpha1` by crd-ref-docs. Do not edit this page by hand.
Run `make docs` instead.

## Packages
{{- range $groupVersions }}
- {{ markdownRenderGVLink . }}
{{- end }}

{{ range $groupVersions }}
{{ template "gvDetails" . }}
{{ end }}

{{- end -}}
