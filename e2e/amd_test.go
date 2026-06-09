package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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
	amdRunningTimeout    = 2 * time.Minute
	amdAnnotationTimeout = 30 * time.Second
	amdAnnotationKey     = "sims.io/assigned-gpus"
	amdHelmUpgradeWait   = 2 * time.Minute
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
	t.Log("waiting for sample pod to be Scheduled")
	if err := waitForScheduled(ctx, cs, sampleNamespace, amdSamplePodName, scheduleTimeout); err != nil {
		t.Fatalf("sample pod never Scheduled: %v", err)
	}

	// 6. Phase 4: pod actually Runs (kubelet allocates amd.com/gpu via
	//    the device-plugin) and the annotator stamps sims.io/assigned-gpus.
	t.Log("waiting for sample pod to reach Running (Phase 4: device-plugin allocates amd.com/gpu)")
	if err := waitForRunning(ctx, cs, sampleNamespace, amdSamplePodName, amdRunningTimeout); err != nil {
		t.Fatalf("sample pod never Running: %v", err)
	}
	t.Log("waiting for sims.io/assigned-gpus annotation from device-plugin annotator")
	val, err := waitForAnnotation(ctx, cs, sampleNamespace, amdSamplePodName, amdAnnotationKey, amdAnnotationTimeout)
	if err != nil {
		t.Fatalf("annotation never set: %v", err)
	}
	if val == "" {
		t.Errorf("annotation %s was empty", amdAnnotationKey)
	} else if !isGPUList(val) {
		t.Errorf("annotation %s = %q, want comma-list of gpu-N", amdAnnotationKey, val)
	}

	// 7. Scale up via helm upgrade — proves the device-plugin is in the
	//    loop, not a static install-time Job.
	t.Log("helm upgrade sims-amd to gpusPerNode=4")
	if err := helmUpgradeAMDGpus(ctx, kc, 4); err != nil {
		t.Fatalf("helm upgrade: %v", err)
	}
	t.Log("waiting for amd.com/gpu capacity to reflect new count")
	if err := waitForWorkerCapacity(ctx, cs, "sims.io/gpu-vendor=amd", "amd.com/gpu", 4, amdHelmUpgradeWait); err != nil {
		t.Fatalf("capacity never bumped to 4: %v", err)
	}

	// 8. Delete the cluster cleanly.
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

// helmUpgradeAMDGpus runs `helm upgrade --reuse-values --set gpusPerNode=N
// --set fake-rocm-gpu-operator.gpusPerNode=N` against the kubeconfig.
// Requires `helm` in PATH; sims doesn't currently expose its own scaling
// CLI, so the e2e shells out directly.
func helmUpgradeAMDGpus(ctx context.Context, kubeconfig []byte, gpus int) error {
	kcFile, err := os.CreateTemp("", "amd-e2e-kubeconfig-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(kcFile.Name()) }()
	if _, err := kcFile.Write(kubeconfig); err != nil {
		return err
	}
	_ = kcFile.Close()

	chartPath := filepath.Join(chartDirAbs, "sims-amd")
	gpusStr := strconv.Itoa(gpus)
	cmd := exec.CommandContext(ctx, "helm", "upgrade",
		"sims-amd", chartPath,
		"--namespace", "gpu-operator",
		"--kubeconfig", kcFile.Name(),
		"--reuse-values",
		"--set", "gpusPerNode="+gpusStr,
		"--set", "fake-rocm-gpu-operator.gpusPerNode="+gpusStr,
		"--wait", "--timeout", "2m",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm upgrade: %w\noutput:\n%s", err, out)
	}
	return nil
}

// waitForRunning polls until the pod's phase reaches Running.
func waitForRunning(ctx context.Context, cs kubernetes.Interface, namespace, name string, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		pod, err := cs.CoreV1().Pods(namespace).Get(deadline, name, metav1.GetOptions{})
		if err == nil && pod.Status.Phase == corev1.PodRunning {
			return nil
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("pod %s/%s not Running within %s: %w", namespace, name, timeout, deadline.Err())
		case <-ticker.C:
		}
	}
}

// waitForAnnotation polls until the pod carries the given annotation key,
// returning its value.
func waitForAnnotation(ctx context.Context, cs kubernetes.Interface, namespace, name, key string, timeout time.Duration) (string, error) {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		pod, err := cs.CoreV1().Pods(namespace).Get(deadline, name, metav1.GetOptions{})
		if err == nil {
			if v, ok := pod.Annotations[key]; ok {
				return v, nil
			}
		}
		select {
		case <-deadline.Done():
			return "", fmt.Errorf("pod %s/%s annotation %s not set within %s: %w", namespace, name, key, timeout, deadline.Err())
		case <-ticker.C:
		}
	}
}

// waitForWorkerCapacity polls until at least one worker selected by
// labelSelector reports capacity[resourceName] >= want.
func waitForWorkerCapacity(ctx context.Context, cs kubernetes.Interface, labelSelector, resourceName string, want int64, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		nodes, err := cs.CoreV1().Nodes().List(deadline, metav1.ListOptions{LabelSelector: labelSelector})
		if err == nil {
			for _, n := range nodes.Items {
				q, ok := n.Status.Capacity[corev1.ResourceName(resourceName)]
				if !ok {
					continue
				}
				got, _ := q.AsInt64()
				if got >= want {
					return nil
				}
			}
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("no worker reports %s >= %d within %s: %w", resourceName, want, timeout, deadline.Err())
		case <-ticker.C:
		}
	}
}

// isGPUList returns true when s parses as a comma-separated list of
// "gpu-<n>" tokens (whitespace-trimmed). Used to validate the
// sims.io/assigned-gpus annotation.
func isGPUList(s string) bool {
	if s == "" {
		return false
	}
	for p := range strings.SplitSeq(s, ",") {
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, "gpu-") {
			return false
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(p, "gpu-")); err != nil {
			return false
		}
	}
	return true
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
