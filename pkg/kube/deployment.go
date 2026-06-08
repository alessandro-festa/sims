package kube

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WaitForDeploymentAvailable blocks until the named Deployment reports the
// Available condition as True, or ctx is done. Returns nil on success;
// the ctx error wrapped with what was being waited on on timeout.
//
// Matches `kubectl wait deploy/<name> --for=condition=Available`.
func WaitForDeploymentAvailable(ctx context.Context, kubeconfig []byte, namespace, name string) error {
	cs, err := newClientset(kubeconfig)
	if err != nil {
		return err
	}
	return waitForDeploymentAvailable(ctx, cs, namespace, name)
}

func waitForDeploymentAvailable(ctx context.Context, cs kubernetes.Interface, namespace, name string) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		d, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
		}
		if err == nil && deploymentAvailable(d) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for deployment %s/%s to be Available: %w", namespace, name, ctx.Err())
		case <-ticker.C:
		}
	}
}

func deploymentAvailable(d *appsv1.Deployment) bool {
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
