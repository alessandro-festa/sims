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
	DefaultWorkers      = 2
	DefaultK8sVersion   = "v1.31.0"
	DefaultRegistryName = "kind-registry"
	DefaultRegistryPort = 5001
)

// Options is the input to Render. Zero values are replaced with the Default* constants above,
// except for Vendor which is required.
type Options struct {
	Vendor       string
	Name         string
	Workers      int
	K8sVersion   string
	Taint        bool
	RegistryName string
	RegistryPort int
}

// Render returns the kind cluster YAML configured for the given Options.
//
// The rendered config:
//   - Names the cluster "sims-<vendor>" when Options.Name is empty.
//   - Creates one control-plane node and Options.Workers worker nodes, all on
//     image kindest/node:<K8sVersion>.
//   - Labels workers with sims.io/gpu-vendor=<vendor> and a vendor-specific
//     "GPU present" label so node selectors can target them.
//   - When Options.Taint is set, adds <vendor>.com/gpu=present:NoSchedule on workers.
//   - Configures containerd to mirror localhost:<RegistryPort> to a registry
//     container reachable inside the kind network as <RegistryName>:<RegistryPort>.
//   - For the NVIDIA vendor, enables the DynamicResourceAllocation feature gate and
//     the resource.k8s.io/v1alpha3 runtime config (required by fake-gpu-operator's
//     DRA plugin on K8s ≥1.31; harmless on older versions but the DRA plugin pods
//     will not become Ready).
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

type templateData struct {
	Name          string
	NodeImage     string
	WorkerIndices []struct{}
	Vendor        string
	PresentLabel  string
	Taint         bool
	RegistryName  string
	RegistryPort  int
	EnableDRA     bool
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
	if o.RegistryName == "" {
		o.RegistryName = DefaultRegistryName
	}
	if o.RegistryPort == 0 {
		o.RegistryPort = DefaultRegistryPort
	}
	if o.Name == "" {
		o.Name = "sims-" + o.Vendor
	}

	present := map[string]string{
		VendorNVIDIA: "nvidia.com/gpu.present",
		VendorAMD:    "feature.node.kubernetes.io/amd-gpu",
	}[o.Vendor]

	return templateData{
		Name:          o.Name,
		NodeImage:     "kindest/node:" + o.K8sVersion,
		WorkerIndices: make([]struct{}, o.Workers),
		Vendor:        o.Vendor,
		PresentLabel:  present,
		Taint:         o.Taint,
		RegistryName:  o.RegistryName,
		RegistryPort:  o.RegistryPort,
		EnableDRA:     o.Vendor == VendorNVIDIA,
	}, nil
}

var tmpl = template.Must(template.New("kind").Parse(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: {{ .Name }}
nodes:
  - role: control-plane
    image: {{ .NodeImage }}
{{- range $i, $_ := .WorkerIndices }}
  - role: worker
    image: {{ $.NodeImage }}
    labels:
      sims.io/gpu-vendor: "{{ $.Vendor }}"
      {{ $.PresentLabel }}: "true"
{{- if $.Taint }}
    taints:
      - key: "{{ $.Vendor }}.com/gpu"
        value: "present"
        effect: NoSchedule
{{- end }}
{{- end }}
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:{{ .RegistryPort }}"]
      endpoint = ["http://{{ .RegistryName }}:{{ .RegistryPort }}"]
{{- if .EnableDRA }}
featureGates:
  DynamicResourceAllocation: true
runtimeConfig:
  resource.k8s.io/v1alpha3: "true"
{{- end }}
`))
