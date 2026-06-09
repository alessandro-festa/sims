{{/*
Shared labels. "app" stays stable across the DS/Service/SA so the
sims-monitoring ServiceMonitor can target the Service reliably.
*/}}
{{- define "fake-dcgm-extras.labels" -}}
app: dcgm-extras-exporter
app.kubernetes.io/name: fake-dcgm-extras
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: sims
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}

{{- define "fake-dcgm-extras.selectorLabels" -}}
app: dcgm-extras-exporter
app.kubernetes.io/name: fake-dcgm-extras
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
