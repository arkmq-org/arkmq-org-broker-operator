{{/*
Expand the name of the chart.
*/}}
{{- define "broker.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Allow the release namespace to be overridden for multi-namespace deployments in combined charts
*/}}
{{- define "broker.namespace" -}}
  {{- if .Values.namespaceOverride -}}
    {{- .Values.namespaceOverride -}}
  {{- else -}}
    {{- .Release.Namespace -}}
  {{- end -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "broker.fullname" -}}
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
{{- define "broker.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "broker.labels" -}}
helm.sh/chart: {{ include "broker.chart" . }}
{{ include "broker.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "broker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "broker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "broker.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "broker.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "broker.credentialsSecretName" -}}
{{ include "broker.fullname" . }}-credentials-secret
{{- end }}

{{- define "broker.jaasConfigSecretName" -}}
{{ include "broker.fullname" . }}-jaas-config
{{- end }}

{{- define "broker.loggingConfigSecretName" -}}
{{ include "broker.fullname" . }}-logging-config
{{- end }}

{{- define "broker.configurationSecretName" -}}
{{- $top := index . 0 -}}
{{- $name := index . 1 -}}
{{ include "broker.fullname" $top }}-{{ required ".Values.configuration.broker[].name is required" $name }}-bp
{{- end }}
