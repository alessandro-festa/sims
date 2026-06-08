package kube

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWaitForDeploymentAvailable_AlreadyAvailable(t *testing.T) {
	cs := fake.NewClientset(deploymentWithCondition("monitoring", "grafana", corev1.ConditionTrue))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForDeploymentAvailable(ctx, cs, "monitoring", "grafana"); err != nil {
		t.Fatalf("waitForDeploymentAvailable: %v", err)
	}
}

func TestWaitForDeploymentAvailable_BecomesAvailable(t *testing.T) {
	prev := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = prev })

	cs := fake.NewClientset(deploymentWithCondition("monitoring", "grafana", corev1.ConditionFalse))
	var wg sync.WaitGroup
	wg.Go(func() {
		time.Sleep(40 * time.Millisecond)
		d := deploymentWithCondition("monitoring", "grafana", corev1.ConditionTrue)
		if _, err := cs.AppsV1().Deployments("monitoring").Update(context.Background(), d, metav1.UpdateOptions{}); err != nil {
			t.Errorf("update: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitForDeploymentAvailable(ctx, cs, "monitoring", "grafana"); err != nil {
		t.Fatalf("waitForDeploymentAvailable: %v", err)
	}
	wg.Wait()
}

func TestWaitForDeploymentAvailable_Timeout(t *testing.T) {
	prev := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = prev })

	cs := fake.NewClientset(deploymentWithCondition("monitoring", "grafana", corev1.ConditionFalse))
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	err := waitForDeploymentAvailable(ctx, cs, "monitoring", "grafana")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}

func TestWaitForDeploymentAvailable_NotFoundKeepsPolling(t *testing.T) {
	prev := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = prev })

	cs := fake.NewClientset()
	var wg sync.WaitGroup
	wg.Go(func() {
		time.Sleep(40 * time.Millisecond)
		_, _ = cs.AppsV1().Deployments("monitoring").Create(context.Background(),
			deploymentWithCondition("monitoring", "grafana", corev1.ConditionTrue),
			metav1.CreateOptions{})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitForDeploymentAvailable(ctx, cs, "monitoring", "grafana"); err != nil {
		t.Fatalf("waitForDeploymentAvailable: %v", err)
	}
	wg.Wait()
}

func deploymentWithCondition(namespace, name string, status corev1.ConditionStatus) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: status},
			},
		},
	}
}
