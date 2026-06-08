package kube

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DetectVendor returns the GPU vendor of a sims cluster by reading the
// sims.io/gpu-vendor label from the first labeled node. Returns an error if
// no node carries the label.
func DetectVendor(ctx context.Context, kubeconfig []byte) (string, error) {
	cs, err := newClientset(kubeconfig)
	if err != nil {
		return "", err
	}
	return detectVendor(ctx, cs)
}

func detectVendor(ctx context.Context, cs kubernetes.Interface) (string, error) {
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: gpuVendorLabel})
	if err != nil {
		return "", fmt.Errorf("list nodes: %w", err)
	}
	for i := range nodes.Items {
		if v := nodes.Items[i].Labels[gpuVendorLabel]; v != "" {
			return v, nil
		}
	}
	return "", errors.New("no node carries the sims.io/gpu-vendor label; is this a sims-managed cluster?")
}
