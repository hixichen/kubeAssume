{{/*
Expand the name of the chart.
*/}}
{{- define "kubeassume.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "kubeassume.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kubeassume.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kubeassume.labels" -}}
helm.sh/chart: {{ include "kubeassume.chart" . }}
{{ include "kubeassume.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kubeassume.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubeassume.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "kubeassume.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "kubeassume.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Public issuer URL
*/}}
{{- define "kubeassume.publicIssuerURL" -}}
{{- if .Values.config.controller.publicIssuerURL }}
{{- .Values.config.controller.publicIssuerURL }}
{{- else if eq .Values.config.publisher.type "s3" }}
{{- printf "https://%s.s3.%s.amazonaws.com" .Values.config.publisher.s3.bucket .Values.config.publisher.s3.region }}
{{- else }}
{{- fail "Cannot determine publicIssuerURL. Please set config.controller.publicIssuerURL or configure a publisher with a derivable URL." }}
{{- end }}
{{- end -}}
