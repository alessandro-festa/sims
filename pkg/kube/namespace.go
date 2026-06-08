package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EnsureNamespace creates the namespace if it doesn't exist, and (whether
// newly created or pre-existing) overwrites the provided labels on it. This
// is the entry point for sims's Helm flow: the chart cannot own its own
// namespace because Helm requires the namespace to exist before it can store
// release metadata in it, and Pod Security Admission needs the labels in
// place before any pod with restricted hostMounts is admitted.
func EnsureNamespace(ctx context.Context, kubeconfig []byte, name string, labels map[string]string) error {
	cs, err := newClientset(kubeconfig)
	if err != nil {
		return err
	}
	return ensureNamespace(ctx, cs, name, labels)
}

func ensureNamespace(ctx context.Context, cs kubernetes.Interface, name string, labels map[string]string) error {
	ns, err := cs.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get namespace %q: %w", name, err)
		}
		_, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		}, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create namespace %q: %w", name, err)
		}
		if err == nil {
			return nil
		}
		// Lost the race; fall through to patch the existing one.
		ns, err = cs.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get namespace %q after create race: %w", name, err)
		}
	}

	if ns.Labels == nil {
		ns.Labels = map[string]string{}
	}
	mutated := false
	for k, v := range labels {
		if ns.Labels[k] != v {
			ns.Labels[k] = v
			mutated = true
		}
	}
	if !mutated {
		return nil
	}
	if _, err := cs.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update namespace %q labels: %w", name, err)
	}
	return nil
}

// DeleteNamespace removes the named namespace. Returns nil if it doesn't
// exist (idempotent). Note: namespace deletion is asynchronous in the API
// server — the namespace enters Terminating and disappears once all its
// resources are gone. This function returns as soon as the delete request
// is accepted; it does NOT wait.
func DeleteNamespace(ctx context.Context, kubeconfig []byte, name string) error {
	cs, err := newClientset(kubeconfig)
	if err != nil {
		return err
	}
	return deleteNamespace(ctx, cs, name)
}

func deleteNamespace(ctx context.Context, cs kubernetes.Interface, name string) error {
	err := cs.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete namespace %q: %w", name, err)
	}
	return nil
}
