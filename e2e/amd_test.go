package e2e

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/alessandro-festa/sims/pkg/cluster"
)

const (
	amdMetricSentinel    = "amd_gpu_junction_temperature"
	amdImageBuildTimeout = 5 * time.Minute
)

// TestAMD_EndToEnd brings up sims-amd, asserts capacity advertisement (both
// status.capacity AND status.allocatable, since the scheduler binds against
// allocatable), and waits for a sample pod to be Scheduled.
//
// Phase 3 only: the pod will NOT reach Running — kubelet has no allocator
// for amd.com/gpu until Phase 4 ships fake-rocm-gpu-operator's device-plugin
// subcommand. waitForScheduled (status.nodeName != "") is the highest bar
// Phase 3 can satisfy.
func TestAMD_EndToEnd(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("E2E=1 required (runs a real kind cluster — takes several minutes)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	t.Cleanup(func() { cleanupCluster(t, amdClusterName) })

	// 1. Build the operator image. The NVIDIA path pulls from ghcr.io;
	//    the AMD path hosts its own image because we built it ourselves.
	t.Log("building fake-rocm-gpu-operator image")
	if err := buildOperatorImage(ctx); err != nil {
		t.Fatalf("build operator image: %v", err)
	}

	// 2. Create the cluster (also brings up the local registry).
	t.Log("creating sims-amd cluster (this takes ~1-2 minutes)")
	createCtx, cancelCreate := context.WithTimeout(ctx, createTimeout)
	defer cancelCreate()
	if _, stderr, err := runSims(createCtx, "gpu", "create",
		"--vendor", "amd", "--workers", "2", "--gpus-per-worker", "2"); err != nil {
		t.Fatalf("sims gpu create failed: %v\nstderr:\n%s", err, stderr)
	}

	// 3. Push the operator image into the local registry so the DaemonSet
	//    can pull it. (Until this runs the DS is in ImagePullBackOff;
	//    capacity-patcher uses bitnami/kubectl so it's unaffected.)
	if _, stderr, err := runSims(ctx, "gpu", "load-image", "fake-rocm-gpu-operator:dev"); err != nil {
		t.Fatalf("sims gpu load-image failed: %v\nstderr:\n%s", err, stderr)
	}

	// 4. Verify capacity advertised on workers.
	kc, err := newKubeconfig(ctx, amdClusterName)
	if err != nil {
		t.Fatalf("fetch kubeconfig: %v", err)
	}
	cs := mustClientset(t, kc)
	assertAMDWorkerCapacity(t, ctx, cs, 2)

	// 5. Apply the sample pod and verify it Schedules onto a node.
	sampleYAML, sampleStderr, err := runSims(ctx, "gpu", "sample", "--vendor", "amd")
	if err != nil {
		t.Fatalf("sims gpu sample failed: %v\nstderr:\n%s", err, sampleStderr)
	}
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal(sampleYAML, pod); err != nil {
		t.Fatalf("parse sample YAML: %v\nyaml:\n%s", err, sampleYAML)
	}
	if _, err := cs.CoreV1().Pods(sampleNamespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create sample pod: %v", err)
	}
	t.Log("waiting for sample pod to be Scheduled (Phase 3: will stay Pending after binding, Phase 4 ships the device-plugin that lets it Run)")
	if err := waitForScheduled(ctx, cs, sampleNamespace, amdSamplePodName, scheduleTimeout); err != nil {
		t.Fatalf("sample pod never Scheduled: %v", err)
	}

	// 6. Delete the cluster cleanly.
	if _, stderr, err := runSims(ctx, "gpu", "delete", "--name", amdClusterName); err != nil {
		t.Fatalf("sims gpu delete failed: %v\nstderr:\n%s", err, stderr)
	}
	provider := cluster.New(nil)
	names, err := provider.List(ctx)
	if err != nil {
		t.Fatalf("list clusters after delete: %v", err)
	}
	if slices.Contains(names, amdClusterName) {
		t.Errorf("cluster %q still listed after delete: %v", amdClusterName, names)
	}
}

// TestAMD_Monitoring_EndToEnd brings up sims-amd with --monitoring, pushes
// the operator image, waits for the AMD dashboard ConfigMap to be loaded by
// Grafana's sidecar, and confirms Prometheus scrapes at least one
// amd_gpu_junction_temperature sample from the metrics-exporter.
func TestAMD_Monitoring_EndToEnd(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("E2E=1 required (runs a real kind cluster + monitoring stack — ~4-6 min)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	t.Cleanup(func() { cleanupCluster(t, amdClusterName) })

	t.Log("building fake-rocm-gpu-operator image")
	if err := buildOperatorImage(ctx); err != nil {
		t.Fatalf("build operator image: %v", err)
	}

	t.Log("creating sims-amd cluster with --monitoring (this takes 3-5 min)")
	createCtx, cancelCreate := context.WithTimeout(ctx, createMonTimeout)
	defer cancelCreate()
	if _, stderr, err := runSims(createCtx, "gpu", "create",
		"--vendor", "amd", "--workers", "2", "--gpus-per-worker", "2", "--monitoring"); err != nil {
		t.Fatalf("sims gpu create --monitoring failed: %v\nstderr:\n%s", err, stderr)
	}

	if _, stderr, err := runSims(ctx, "gpu", "load-image", "fake-rocm-gpu-operator:dev"); err != nil {
		t.Fatalf("sims gpu load-image failed: %v\nstderr:\n%s", err, stderr)
	}

	kc, err := newKubeconfig(ctx, amdClusterName)
	if err != nil {
		t.Fatalf("fetch kubeconfig: %v", err)
	}
	cs := mustClientset(t, kc)

	// Dashboard CM exists + Grafana sidecar picked it up.
	t.Log("verifying AMD dashboard CM and Grafana sidecar load")
	if _, err := cs.CoreV1().ConfigMaps(monitoringNamespace).Get(ctx, "sims-monitoring-amd-gpu-dashboard", metav1.GetOptions{}); err != nil {
		t.Errorf("AMD dashboard ConfigMap missing: %v", err)
	}
	if err := waitForSidecarLog(ctx, cs, "amd-gpu.json", dashboardLoadedTimeout); err != nil {
		t.Errorf("grafana sidecar never logged amd-gpu.json: %v", err)
	}

	// Prometheus scraping the AMD metric.
	t.Log("waiting for Prometheus to report an amd_gpu_junction_temperature sample")
	if err := waitForPrometheusMetric(ctx, cs, amdMetricSentinel, dcgmSampleTimeout); err != nil {
		t.Errorf("prometheus never saw %s: %v", amdMetricSentinel, err)
	}
}

// buildOperatorImage runs the operator's `make image` target, which docker
// buildx-builds + loads fake-rocm-gpu-operator:dev into the local daemon.
func buildOperatorImage(ctx context.Context) error {
	imgCtx, cancel := context.WithTimeout(ctx, amdImageBuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(imgCtx, "make", "-C", "../operators/fake-rocm-gpu-operator", "image")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// assertAMDWorkerCapacity verifies both status.capacity AND
// status.allocatable carry amd.com/gpu. The scheduler binds against
// allocatable; capacity-patcher writes both because kubelet computes
// allocatable for extended resources from device-plugin registrations only.
func assertAMDWorkerCapacity(t *testing.T, ctx context.Context, cs kubernetes.Interface, want int64) {
	t.Helper()
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: "sims.io/gpu-vendor=amd"})
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	if len(nodes.Items) == 0 {
		t.Fatal("no worker nodes carry the sims.io/gpu-vendor=amd label")
	}
	for _, n := range nodes.Items {
		check := func(field string, m corev1.ResourceList) {
			q, ok := m["amd.com/gpu"]
			if !ok {
				t.Errorf("node %q: %s lacks amd.com/gpu", n.Name, field)
				return
			}
			got, _ := q.AsInt64()
			if got < want {
				t.Errorf("node %q: %s.amd.com/gpu = %d, want >= %d", n.Name, field, got, want)
			}
		}
		check("capacity", n.Status.Capacity)
		check("allocatable", n.Status.Allocatable)
	}
	// Final defensive check: the "Scheduled but not Running" Phase 3
	// outcome relies on allocatable being present; surface a hint if it
	// somehow isn't.
	for _, n := range nodes.Items {
		if _, ok := n.Status.Allocatable["amd.com/gpu"]; !ok {
			t.Logf("hint: without status.allocatable.amd.com/gpu, the scheduler will report 'Insufficient amd.com/gpu' and waitForScheduled will time out — check charts/sims-amd/templates/capacity-patcher-job.yaml")
			break
		}
	}
}
