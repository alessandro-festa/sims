// Package topology owns the schema + read/write helpers for the
// "topology" ConfigMap that maps GPU IDs to the pods consuming them.
//
// The status-updater writes the CM whenever pods with the
// sims.io/assigned-gpus annotation come and go; the metrics-exporter
// reads it on every scrape so per-pod gauge labels stay in sync.
//
// CM schema (single key "topology.yaml"):
//
//	nodes:
//	  <node-name>:
//	    - gpu_id: gpu-0
//	      pod_namespace: default
//	      pod_name: foo
//	      container: payload
package topology

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

const (
	// ConfigMapName is the well-known name the operator uses for the
	// topology CM. Lives in the chart's release namespace.
	ConfigMapName = "topology"

	// DataKey is the ConfigMap data key holding the marshalled YAML body.
	DataKey = "topology.yaml"
)

// Assignment is one GPU ↔ container binding inside the topology CM.
type Assignment struct {
	GPUID        string `json:"gpu_id" yaml:"gpu_id"`
	PodNamespace string `json:"pod_namespace" yaml:"pod_namespace"`
	PodName      string `json:"pod_name" yaml:"pod_name"`
	Container    string `json:"container" yaml:"container"`
}

// Topology is the marshalled CM body: a map of node name → assignments
// on that node. Missing nodes or empty slices imply "all GPUs idle".
type Topology struct {
	Nodes map[string][]Assignment `json:"nodes" yaml:"nodes"`
}

// Empty returns a Topology with an initialised (non-nil) Nodes map.
// Callers don't have to nil-check before adding entries.
func Empty() *Topology {
	return &Topology{Nodes: map[string][]Assignment{}}
}

// Load reads the topology ConfigMap from the given namespace. Returns an
// Empty Topology (not an error) when the CM doesn't exist yet — the
// exporter races the status-updater on first install and shouldn't panic
// in that window.
func Load(ctx context.Context, cs kubernetes.Interface, namespace string) (*Topology, error) {
	cm, err := cs.CoreV1().ConfigMaps(namespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Empty(), nil
		}
		return nil, fmt.Errorf("get topology CM: %w", err)
	}
	body, ok := cm.Data[DataKey]
	if !ok || body == "" {
		return Empty(), nil
	}
	t := Empty()
	if err := yaml.Unmarshal([]byte(body), t); err != nil {
		return nil, fmt.Errorf("unmarshal topology CM: %w", err)
	}
	if t.Nodes == nil {
		t.Nodes = map[string][]Assignment{}
	}
	return t, nil
}

// Save writes the topology to the CM, creating it if it doesn't exist
// yet (the chart templates an empty CM at install so this is mostly
// updates).
func Save(ctx context.Context, cs kubernetes.Interface, namespace string, t *Topology) error {
	if t == nil {
		return errors.New("save: topology is nil")
	}
	if t.Nodes == nil {
		t.Nodes = map[string][]Assignment{}
	}
	body, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal topology: %w", err)
	}

	existing, getErr := cs.CoreV1().ConfigMaps(namespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return fmt.Errorf("get topology CM: %w", getErr)
	}
	if apierrors.IsNotFound(getErr) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ConfigMapName,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/part-of":    "sims",
					"app.kubernetes.io/managed-by": "fake-rocm-gpu-operator",
				},
			},
			Data: map[string]string{DataKey: string(body)},
		}
		if _, err := cs.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create topology CM: %w", err)
		}
		return nil
	}
	if existing.Data == nil {
		existing.Data = map[string]string{}
	}
	if existing.Data[DataKey] == string(body) {
		return nil // no-op; avoids needless updates on every reconcile.
	}
	existing.Data[DataKey] = string(body)
	if _, err := cs.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update topology CM: %w", err)
	}
	return nil
}

// FindAssignment returns the Assignment for (node, gpuID) or nil if the
// GPU is idle. O(N) over the node's GPU count — fine for fleet sizes
// where N < 50; tweak if larger.
func (t *Topology) FindAssignment(node, gpuID string) *Assignment {
	for i := range t.Nodes[node] {
		if t.Nodes[node][i].GPUID == gpuID {
			return &t.Nodes[node][i]
		}
	}
	return nil
}
