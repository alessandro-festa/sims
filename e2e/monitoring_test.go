package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

const (
	monitoringNamespace    = "monitoring"
	prometheusServiceName  = "sims-monitoring-kube-prome-prometheus"
	prometheusServicePort  = "9090"
	dcgmUtilMetric         = "DCGM_FI_DEV_GPU_UTIL"
	dcgmTempMetric         = "DCGM_FI_DEV_GPU_TEMP"
	dcgmSampleTimeout      = 90 * time.Second
	dashboardLoadedTimeout = 90 * time.Second
)

func TestNVIDIA_Monitoring_EndToEnd(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("E2E=1 required (runs a real kind cluster + monitoring stack — ~3-5 min)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	t.Cleanup(func() { cleanupCluster(t, clusterName) })

	// Pre-build the Phase 7 DCGM extras sidecar image. The chart pulls
	// it from the local registry once we push (step 1.5 below).
	t.Log("building fake-dcgm-extras image")
	if err := buildDCGMExtrasImage(ctx); err != nil {
		t.Fatalf("build fake-dcgm-extras image: %v", err)
	}

	// 1. Create with --monitoring (takes ~2-4 min on first run).
	t.Log("creating sims-nvidia cluster with --monitoring (this takes 2-4 min)")
	createCtx, cancelCreate := context.WithTimeout(ctx, createMonTimeout)
	defer cancelCreate()
	if _, stderr, err := runSims(createCtx, "gpu", "create",
		"--vendor", "nvidia", "--workers", "2", "--gpus-per-worker", "2", "--monitoring"); err != nil {
		t.Fatalf("sims gpu create --monitoring failed: %v\nstderr:\n%s", err, stderr)
	}

	// 1.5. Push fake-dcgm-extras to the local registry. Until this runs
	// the DS is in ImagePullBackOff; DCGM_FI_DEV_GPU_UTIL still works
	// (emitted by fake-gpu-operator's status-exporter), but
	// DCGM_FI_DEV_GPU_TEMP won't appear until the extras image is up.
	if _, stderr, err := runSims(ctx, "gpu", "load-image", "fake-dcgm-extras:dev"); err != nil {
		t.Fatalf("sims gpu load-image (extras): %v\nstderr:\n%s", err, stderr)
	}

	kc, err := newKubeconfig(ctx, clusterName)
	if err != nil {
		t.Fatalf("fetch kubeconfig: %v", err)
	}
	cs := mustClientset(t, kc)

	// 2. Apply the sample pod so DCGM_FI_DEV_GPU_UTIL has something to report.
	t.Log("applying sample pod")
	sampleYAML, _, err := runSims(ctx, "gpu", "sample", "--vendor", "nvidia")
	if err != nil {
		t.Fatalf("sims gpu sample: %v", err)
	}
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal(sampleYAML, pod); err != nil {
		t.Fatalf("parse sample YAML: %v", err)
	}
	if _, err := cs.CoreV1().Pods(sampleNamespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create sample pod: %v", err)
	}
	if err := waitForScheduled(ctx, cs, sampleNamespace, samplePodName, scheduleTimeout); err != nil {
		t.Fatalf("sample pod never Scheduled: %v", err)
	}

	// 3. Confirm the dashboard CM exists and Grafana's sidecar loaded it.
	// (Grafana's /api/search needs basic auth which the API server's
	// service-proxy doesn't pass; sidecar log is the cleanest signal.)
	t.Log("verifying dashboard CM and Grafana sidecar load")
	if _, err := cs.CoreV1().ConfigMaps(monitoringNamespace).Get(ctx, "sims-monitoring-nvidia-dcgm-dashboard", metav1.GetOptions{}); err != nil {
		t.Errorf("dashboard ConfigMap missing: %v", err)
	}
	if err := waitForSidecarLog(ctx, cs, "nvidia-dcgm.json", dashboardLoadedTimeout); err != nil {
		t.Errorf("grafana sidecar never logged dashboard load: %v", err)
	}

	// 4. Confirm Prometheus is scraping at least one DCGM_FI_DEV_GPU_UTIL sample.
	t.Log("waiting for Prometheus to report a DCGM_FI_DEV_GPU_UTIL sample")
	if err := waitForPrometheusMetric(ctx, cs, dcgmUtilMetric, dcgmSampleTimeout); err != nil {
		t.Errorf("prometheus never saw %s: %v", dcgmUtilMetric, err)
	}

	// 5. Phase 7: extras sidecar should be feeding DCGM_FI_DEV_GPU_TEMP
	// into Prometheus. The vendored dashboard's temperature panel
	// depends on this metric; before Phase 7 it stayed empty.
	t.Log("waiting for Prometheus to report a DCGM_FI_DEV_GPU_TEMP sample (Phase 7 extras sidecar)")
	if err := waitForPrometheusMetric(ctx, cs, dcgmTempMetric, dcgmSampleTimeout); err != nil {
		t.Errorf("prometheus never saw %s: %v", dcgmTempMetric, err)
	}
}

// buildDCGMExtrasImage runs the fake-dcgm-extras Makefile's `image`
// target, equivalent to buildOperatorImage in amd_test.go but for the
// Phase 7 sidecar binary.
func buildDCGMExtrasImage(ctx context.Context) error {
	imgCtx, cancel := context.WithTimeout(ctx, amdImageBuildTimeout) // 5 min, same budget
	defer cancel()
	cmd := exec.CommandContext(imgCtx, "make", "-C", "../operators/fake-dcgm-extras", "image")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// serviceProxyGet does an in-cluster GET via the Kubernetes API server's
// service proxy. Avoids needing a local port-forward in the test.
func serviceProxyGet(ctx context.Context, cs kubernetes.Interface, namespace, service, port, path string, params map[string]string) ([]byte, error) {
	req := cs.CoreV1().Services(namespace).
		ProxyGet("http", service, port, path, params)
	return req.DoRaw(ctx)
}

// waitForSidecarLog tails the Grafana sidecar container's logs until it
// reports writing the given dashboard JSON filename to disk — that's the
// moment Grafana's filesystem-watching provisioner picks it up. Side-steps
// needing basic auth against Grafana's HTTP API.
func waitForSidecarLog(ctx context.Context, cs kubernetes.Interface, filename string, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		pods, err := cs.CoreV1().Pods(monitoringNamespace).List(deadline, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=grafana",
		})
		if err == nil && len(pods.Items) > 0 {
			for _, p := range pods.Items {
				logs, err := readContainerLog(deadline, cs, p.Namespace, p.Name, "grafana-sc-dashboard")
				if err == nil && strings.Contains(logs, filename) {
					return nil
				}
			}
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("grafana sidecar never logged %s within %s: %w", filename, timeout, deadline.Err())
		case <-ticker.C:
		}
	}
}

func readContainerLog(ctx context.Context, cs kubernetes.Interface, namespace, pod, container string) (string, error) {
	req := cs.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{Container: container})
	stream, err := req.Stream(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	defer func() { _ = stream.Close() }()
	b, err := io.ReadAll(stream)
	return string(b), err
}

func waitForPrometheusMetric(ctx context.Context, cs kubernetes.Interface, metric string, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		body, err := serviceProxyGet(deadline, cs, monitoringNamespace, prometheusServiceName, prometheusServicePort, "api/v1/query", map[string]string{"query": metric})
		if err == nil {
			var resp struct {
				Status string `json:"status"`
				Data   struct {
					Result []json.RawMessage `json:"result"`
				} `json:"data"`
			}
			if jerr := json.Unmarshal(body, &resp); jerr == nil && resp.Status == "success" && len(resp.Data.Result) > 0 {
				return nil
			}
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("metric %s not present within %s: %w (last body: %s)", metric, timeout, deadline.Err(), truncate(body, 200))
		case <-ticker.C:
		}
	}
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
