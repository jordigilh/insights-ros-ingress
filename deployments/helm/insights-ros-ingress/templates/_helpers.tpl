{{/*
Expand the name of the chart.
*/}}
{{- define "insights-ros-ingress.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "insights-ros-ingress.fullname" -}}
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
{{- define "insights-ros-ingress.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "insights-ros-ingress.labels" -}}
helm.sh/chart: {{ include "insights-ros-ingress.chart" . }}
{{ include "insights-ros-ingress.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "insights-ros-ingress.selectorLabels" -}}
app.kubernetes.io/name: {{ include "insights-ros-ingress.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "insights-ros-ingress.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "insights-ros-ingress.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create image name
*/}}
{{- define "insights-ros-ingress.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}

{{/*
Create common annotations
*/}}
{{- define "insights-ros-ingress.annotations" -}}
{{- with .Values.commonAnnotations }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Create storage secret name
*/}}
{{- define "insights-ros-ingress.storageSecretName" -}}
{{- default (printf "%s-storage" (include "insights-ros-ingress.fullname" .)) .Values.storage.existingSecret }}
{{- end }}

{{/*
Create auth secret name
*/}}
{{- define "insights-ros-ingress.authSecretName" -}}
{{- default (printf "%s-auth" (include "insights-ros-ingress.fullname" .)) .Values.auth.existingSecret }}
{{- end }}

{{/*
Create kafka secret name
*/}}
{{- define "insights-ros-ingress.kafkaSecretName" -}}
{{- default (printf "%s-kafka" (include "insights-ros-ingress.fullname" .)) .Values.kafka.security.existingSecret }}
{{- end }}

{{/*
Create route host
*/}}
{{- define "insights-ros-ingress.routeHost" -}}
{{- if .Values.route.host }}
{{- .Values.route.host }}
{{- else }}
{{- printf "%s.%s" (include "insights-ros-ingress.fullname" .) "apps.cluster.local" }}
{{- end }}
{{- end }}