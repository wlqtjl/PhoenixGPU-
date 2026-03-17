{{/*
PhoenixGPU Helm Chart Helpers
*/}}

{{- define "phoenixgpu.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "phoenixgpu.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "phoenixgpu.labels" -}}
helm.sh/chart: {{ include "phoenixgpu.chart" . }}
{{ include "phoenixgpu.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "phoenixgpu.selectorLabels" -}}
app.kubernetes.io/name: phoenixgpu
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
