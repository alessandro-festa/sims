package config

import (
	"bytes"
	"fmt"
	"text/template"
)

// Vendor identifies a simulated GPU vendor.
const (
	VendorNVIDIA = "nvidia"
	VendorAMD    = "amd"
)

// Defaults applied by Render when the corresponding Options field is its zero value.
const (
	DefaultWorkers    = 2
	DefaultK8sVersion = "v1.31.0"
)

// Options is the input to Render. Zero values are replaced with the Default* constants above,
// except for Vendor which is required.
type Options struct {
	Vendor         string
	Name           string
	Workers        int
	K8sVersion     string
	TaintedWorkers int
}

// GPUWorkers returns the number of workers that will advertise GPU capacity.
// When TaintedWorkers > 0, only those workers are GPU nodes. Otherwise all
// workers are GPU nodes (the default, backward-compatible behavior).
func (o Options) GPUWorkers() int {
	if o.TaintedWorkers > 0 {
		return o.TaintedWorkers
	}
	if o.Workers > 0 {
		return o.Workers
	}
	return DefaultWorkers
}

// Render returns the kind cluster YAML configured for the given Options.
//
// The rendered config:
//   - Names the cluster "sims-<vendor>" when Options.Name is empty.
//   - Creates one control-plane node and Options.Workers worker nodes, all on
//     image kindest/node:<K8sVersion>.
//   - When TaintedWorkers == 0 (default): all workers get GPU labels (no taint).
//   - When TaintedWorkers > 0: only the first N workers get GPU labels + taint;
//     remaining workers are plain compute nodes.
//   - For the NVIDIA vendor, enables the DynamicResourceAllocation feature gate and
//     the resource.k8s.io/v1alpha3 runtime config.
func Render(o Options) ([]byte, error) {
	d, err := buildTemplateData(o)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, d); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), nil
}

type workerData struct {
	HasGPU  bool
	Tainted bool
}

type templateData struct {
	Name         string
	NodeImage    string
	Workers      []workerData
	Vendor       string
	PresentLabel string
	ExtraLabels  map[string]string
	TaintKey     string
	EnableDRA    bool
}

func buildTemplateData(o Options) (templateData, error) {
	switch o.Vendor {
	case VendorNVIDIA, VendorAMD:
	default:
		return templateData{}, fmt.Errorf("invalid vendor %q (must be %q or %q)", o.Vendor, VendorNVIDIA, VendorAMD)
	}
	if o.Workers <= 0 {
		o.Workers = DefaultWorkers
	}
	if o.K8sVersion == "" {
		o.K8sVersion = DefaultK8sVersion
	}
	if o.Name == "" {
		o.Name = "sims-" + o.Vendor
	}

	present := map[string]string{
		VendorNVIDIA: "nvidia.com/gpu.present",
		VendorAMD:    "feature.node.kubernetes.io/amd-gpu",
	}[o.Vendor]

	var extraLabels map[string]string
	if o.Vendor == VendorNVIDIA {
		extraLabels = map[string]string{
			"nvidia.com/gpu.deploy.device-plugin": "true",
			"nvidia.com/gpu.deploy.dcgm-exporter": "true",
			"run.ai/simulated-gpu-node-pool":      "default",
		}
	}

	workers := make([]workerData, o.Workers)
	for i := range workers {
		if o.TaintedWorkers > 0 {
			// Selective mode: only tainted workers are GPU nodes.
			workers[i].HasGPU = i < o.TaintedWorkers
			workers[i].Tainted = i < o.TaintedWorkers
		} else {
			// Default mode: all workers are GPU nodes, no taint.
			workers[i].HasGPU = true
		}
	}

	return templateData{
		Name:         o.Name,
		NodeImage:    "kindest/node:" + o.K8sVersion,
		Workers:      workers,
		Vendor:       o.Vendor,
		PresentLabel: present,
		ExtraLabels:  extraLabels,
		TaintKey:     o.Vendor + ".com/gpu",
		EnableDRA:    o.Vendor == VendorNVIDIA,
	}, nil
}

var tmpl = template.Must(template.New("kind").Parse(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: {{ .Name }}
nodes:
  - role: control-plane
    image: {{ .NodeImage }}
{{- range $i, $w := .Workers }}
  - role: worker
    image: {{ $.NodeImage }}
{{- if $w.HasGPU }}
    labels:
      sims.io/gpu-vendor: "{{ $.Vendor }}"
      {{ $.PresentLabel }}: "true"
{{- range $k, $v := $.ExtraLabels }}
      {{ $k }}: "{{ $v }}"
{{- end }}
{{- if $w.Tainted }}
    kubeadmConfigPatches:
      - |
        kind: JoinConfiguration
        nodeRegistration:
          taints:
            - key: "{{ $.TaintKey }}"
              value: "present"
              effect: NoSchedule
{{- end }}
{{- end }}
{{- end }}
{{- if .EnableDRA }}
featureGates:
  DynamicResourceAllocation: true
runtimeConfig:
  resource.k8s.io/v1alpha3: "true"
{{- end }}
`))
