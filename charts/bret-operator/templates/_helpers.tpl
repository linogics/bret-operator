{{/* Chart name, trimmed to 63 characters. */}}
{{- define "bret-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Release-scoped name. Used as prefix for all cluster-scoped objects. */}}
{{- define "bret-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "bret-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Labels shared by every object. */}}
{{- define "bret-operator.labels" -}}
helm.sh/chart: {{ include "bret-operator.chart" . }}
{{ include "bret-operator.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/* Labels used in selectors. Must not change between upgrades. */}}
{{- define "bret-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "bret-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{- define "bret-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "bret-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "bret-operator.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}

{{/*
Comma list of the enabled providers.
*/}}
{{- define "bret-operator.providers" -}}
{{- $p := list -}}
{{- range $name, $on := .Values.providers }}{{ if $on }}{{ $p = append $p $name }}{{ end }}{{ end -}}
{{- join "," (sortAlpha $p) -}}
{{- end }}
