{{/*
Common labels applied to every resource. The "app" label is the join key the
sims-monitoring ServiceMonitor uses to find the metrics-exporter Service —
keep it stable.
*/}}
{{- define "fake-rocm-gpu-operator.labels" -}}
app: amd-device-metrics-exporter
app.kubernetes.io/name: fake-rocm-gpu-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: sims
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}

{{- define "fake-rocm-gpu-operator.selectorLabels" -}}
app: amd-device-metrics-exporter
app.kubernetes.io/name: fake-rocm-gpu-operator
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
