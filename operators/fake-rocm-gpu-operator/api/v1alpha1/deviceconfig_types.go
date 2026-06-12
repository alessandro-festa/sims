package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeviceConfigSpec mirrors a trimmed-but-faithful subset of
// ROCm/gpu-operator's DeviceConfig CRD spec. Fields we don't act on are
// still accepted (so real manifests apply unmodified) but quietly
// ignored — the package-level doc on each field calls this out.
type DeviceConfigSpec struct {
	// Driver controls kernel-module loading. Accepted for compatibility
	// with real ROCm/gpu-operator manifests; sims has no kernel-module
	// management (kind containers share the host kernel) so the field
	// is parsed and otherwise ignored.
	// +optional
	Driver DriverSpec `json:"driver,omitempty"`

	// DevicePlugin controls the kubelet device-plugin DaemonSet
	// (sims's fake one — see operators/fake-rocm-gpu-operator/internal/
	// deviceplugin). Setting Enable to false stops advertising
	// amd.com/gpu capacity within ~30s.
	// +optional
	DevicePlugin DevicePluginSpec `json:"devicePlugin,omitempty"`

	// MetricsExporter controls the Prometheus exporter DaemonSet.
	// Setting Enable to false stops the amd_gpu_* metric flow.
	// +optional
	MetricsExporter MetricsExporterSpec `json:"metricsExporter,omitempty"`

	// NodeSelector restricts every child DS+Deploy to nodes matching.
	// Defaults to sims's kind-vendor label `sims.io/gpu-vendor: amd`
	// when empty.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations applied to every child DS+Deploy PodSpec so they can
	// schedule on tainted GPU nodes.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// CommonConfig collects fields shared by every child workload.
	// Accepted for upstream compatibility; sims uses InitContainerImage
	// only as documentation today.
	// +optional
	CommonConfig CommonConfigSpec `json:"commonConfig,omitempty"`

	// ComputePartition controls AMD CPX/SPX partition emulation: SPX
	// presents each physical GPU as 1 device (the default), CPX presents
	// each physical GPU as Count smaller partitions. Mirrors the
	// partitioning surface on real MI300X-class hardware. Switching
	// modes is a live operation — the reconciler updates the device-
	// plugin DS, capacity changes within ~30s.
	// +optional
	ComputePartition ComputePartitionSpec `json:"computePartition,omitempty"`
}

// DriverSpec — see DeviceConfigSpec.Driver.
type DriverSpec struct {
	// +optional
	Enable *bool `json:"enable,omitempty"`
}

// DevicePluginSpec — see DeviceConfigSpec.DevicePlugin.
type DevicePluginSpec struct {
	// +optional
	Enable *bool `json:"enable,omitempty"`
	// +optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`
}

// MetricsExporterSpec — see DeviceConfigSpec.MetricsExporter.
type MetricsExporterSpec struct {
	// +optional
	Enable *bool `json:"enable,omitempty"`
}

// CommonConfigSpec — see DeviceConfigSpec.CommonConfig.
type CommonConfigSpec struct {
	// +optional
	InitContainerImage string `json:"initContainerImage,omitempty"`
}

// ComputePartitionSpec — see DeviceConfigSpec.ComputePartition.
type ComputePartitionSpec struct {
	// Mode selects the partition style: "spx" (single partition per
	// physical GPU, the default) or "cpx" (Count partitions per physical
	// GPU). Anything else is rejected by the CRD enum.
	// +kubebuilder:validation:Enum=spx;cpx
	// +kubebuilder:default=spx
	// +optional
	Mode string `json:"mode,omitempty"`

	// Count is the number of compute partitions per physical GPU when
	// Mode=cpx. Ignored (treated as 1) when Mode=spx. Real MI300X
	// supports up to 8.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=8
	// +kubebuilder:default=1
	// +optional
	Count int32 `json:"count,omitempty"`
}

// DeviceConfigStatus reports what the reconciler last observed about
// each workload it manages, plus a generic conditions field for
// readiness/error reporting.
type DeviceConfigStatus struct {
	// +optional
	DevicePluginReady bool `json:"devicePluginReady,omitempty"`
	// +optional
	MetricsExporterReady bool `json:"metricsExporterReady,omitempty"`

	// Conditions is the standard kubebuilder Conditions slice. The
	// Reconciler sets a Ready condition based on the *Ready fields.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"deviceConfigConditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=devcfg
// +kubebuilder:printcolumn:name="DevicePlugin",type=boolean,JSONPath=`.status.devicePluginReady`
// +kubebuilder:printcolumn:name="MetricsExporter",type=boolean,JSONPath=`.status.metricsExporterReady`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DeviceConfig is the Schema for the deviceconfigs API. Cluster-scoped
// because in real ROCm/gpu-operator one DeviceConfig drives the whole
// cluster's GPU stack.
type DeviceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeviceConfigSpec   `json:"spec,omitempty"`
	Status DeviceConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DeviceConfigList contains a list of DeviceConfig.
type DeviceConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeviceConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DeviceConfig{}, &DeviceConfigList{})
}
