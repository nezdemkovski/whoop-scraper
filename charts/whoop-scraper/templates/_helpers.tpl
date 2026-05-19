{{- define "whoop-scraper.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "whoop-scraper.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "whoop-scraper.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "whoop-scraper.labels" -}}
helm.sh/chart: {{ include "whoop-scraper.chart" . }}
app.kubernetes.io/name: {{ include "whoop-scraper.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "whoop-scraper.selectorLabels" -}}
app.kubernetes.io/name: {{ include "whoop-scraper.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "whoop-scraper.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "whoop-scraper.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "whoop-scraper.secretName" -}}
{{- default (printf "%s-env" (include "whoop-scraper.fullname" .)) .Values.secret.name -}}
{{- end -}}

{{- define "whoop-scraper.env" -}}
{{- range $name, $value := .Values.env }}
{{- if ne (toString $value) "" }}
- name: {{ $name }}
  value: {{ $value | quote }}
{{- end }}
{{- end }}
{{- with .Values.extraEnv }}
{{- toYaml . }}
{{- end }}
{{- end -}}
