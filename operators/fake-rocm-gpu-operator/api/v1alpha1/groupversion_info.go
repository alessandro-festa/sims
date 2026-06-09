// Package v1alpha1 contains API Schema definitions for the
// amd.sims.io v1alpha1 API group — sims's fake mirror of the real
// ROCm/gpu-operator DeviceConfig CRD.
//
// +kubebuilder:object:generate=true
// +groupName=amd.sims.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion is group version used to register these objects.
//
// We use amd.sims.io (not amd.com) intentionally: the goal is a
// surface-compatible fake, not a hostile takeover of the real AMD
// group name. Users wanting the real surface set apiVersion:
// amd.com/v1alpha1 on their manifests and sims rejects them — clear
// signal that a real cluster behaves differently.
var GroupVersion = schema.GroupVersion{Group: "amd.sims.io", Version: "v1alpha1"}

// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

// AddToScheme adds the types in this group-version to the given scheme.
var AddToScheme = SchemeBuilder.AddToScheme
