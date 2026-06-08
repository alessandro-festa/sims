package kube

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterSnapshot is the data backing `sims gpu status`. Sourced live from
// the cluster — nothing is cached.
type ClusterSnapshot struct {
	// Vendor is "nvidia" or "amd", inferred from the sims.io/gpu-vendor
	// label on the first worker that carries one. Empty if no worker has
	// the label (e.g. a non-sims cluster).
	Vendor string

	// K8sVersion is the kubelet version reported by the first node listed
	// (typically the control-plane). Empty if the cluster has no nodes.
	K8sVersion string

	Nodes   []NodeSnapshot
	GPUPods []PodSnapshot
}

// NodeSnapshot describes one node's role and advertised GPU capacity.
type NodeSnapshot struct {
	Name        string
	Role        string // "control-plane" or "worker"
	GPUCapacity int    // 0 if the GPU resource is not advertised
}

// PodSnapshot describes one Pod that requested the vendor's GPU resource.
type PodSnapshot struct {
	Namespace string
	Name      string
	Node      string
	GPUs      int    // total across containers
	Phase     string // corev1.PodPhase as string
}

// Snapshot collects the data needed by `sims gpu status` from the cluster
// described by kubeconfig. Vendor is auto-detected from worker labels.
func Snapshot(ctx context.Context, kubeconfig []byte) (*ClusterSnapshot, error) {
	cs, err := newClientset(kubeconfig)
	if err != nil {
		return nil, err
	}
	return snapshot(ctx, cs)
}

func snapshot(ctx context.Context, cs kubernetes.Interface) (*ClusterSnapshot, error) {
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	snap := &ClusterSnapshot{}
	for i := range nodes.Items {
		n := &nodes.Items[i]
		if snap.K8sVersion == "" {
			snap.K8sVersion = n.Status.NodeInfo.KubeletVersion
		}
		if snap.Vendor == "" {
			if v := n.Labels[gpuVendorLabel]; v != "" {
				snap.Vendor = v
			}
		}
	}

	gpuResource := ""
	if snap.Vendor != "" {
		gpuResource = snap.Vendor + ".com/gpu"
	}

	for i := range nodes.Items {
		n := &nodes.Items[i]
		role := "worker"
		if _, ok := n.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = "control-plane"
		}
		gpus := 0
		if gpuResource != "" {
			if q, ok := n.Status.Capacity[corev1.ResourceName(gpuResource)]; ok {
				if v, ok := q.AsInt64(); ok {
					gpus = int(v)
				}
			}
		}
		snap.Nodes = append(snap.Nodes, NodeSnapshot{Name: n.Name, Role: role, GPUCapacity: gpus})
	}

	if gpuResource != "" {
		pods, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list pods: %w", err)
		}
		for i := range pods.Items {
			p := &pods.Items[i]
			gpus := podGPURequest(p, corev1.ResourceName(gpuResource))
			if gpus == 0 {
				continue
			}
			snap.GPUPods = append(snap.GPUPods, PodSnapshot{
				Namespace: p.Namespace,
				Name:      p.Name,
				Node:      p.Spec.NodeName,
				GPUs:      gpus,
				Phase:     string(p.Status.Phase),
			})
		}
		sort.Slice(snap.GPUPods, func(i, j int) bool {
			if snap.GPUPods[i].Namespace != snap.GPUPods[j].Namespace {
				return snap.GPUPods[i].Namespace < snap.GPUPods[j].Namespace
			}
			return snap.GPUPods[i].Name < snap.GPUPods[j].Name
		})
	}

	return snap, nil
}

func podGPURequest(p *corev1.Pod, resource corev1.ResourceName) int {
	total := 0
	for c := range p.Spec.Containers {
		if q, ok := p.Spec.Containers[c].Resources.Limits[resource]; ok {
			if v, ok := q.AsInt64(); ok {
				total += int(v)
			}
		}
	}
	return total
}
