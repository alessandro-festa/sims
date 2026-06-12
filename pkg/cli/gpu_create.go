package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
	"github.com/alessandro-festa/sims/pkg/config"
	"github.com/alessandro-festa/sims/pkg/helm"
	"github.com/alessandro-festa/sims/pkg/kube"
	"github.com/alessandro-festa/sims/pkg/simsconfig"
)

const (
	chartReleaseNVIDIA = "sims-nvidia"
	chartReleaseAMD    = "sims-amd"
	chartNamespace     = "gpu-operator"
	defaultChartDir    = "charts"
	chartDirEnvVar     = "SIMS_CHART_DIR"
	gpuResourceNVIDIA  = "nvidia.com/gpu"
	gpuResourceAMD     = "amd.com/gpu"
	capacityWaitWindow = 2 * time.Minute

	monitoringChartName  = "sims-monitoring"
	monitoringRelease    = "sims-monitoring"
	monitoringNamespace  = "monitoring"
	monitoringWaitWindow = 4 * time.Minute
	// grafanaDeploymentSuffix matches kube-prometheus-stack's default
	// naming: <release>-grafana.
	grafanaDeploymentSuffix = "-grafana"
)

type createOpts struct {
	vendor         string
	name           string
	workers        int
	gpusPerWorker  int
	k8sVersion     string
	withMonitoring bool
	taint          bool
	configPath     string

	productName    string
	gpuMemoryBytes int64
	migProfile         string
	partitionMode      string
	partitionCount     int
	defaultUtilization string
}

func newGPUCreateCmd() *cobra.Command {
	var o createOpts
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a kind cluster simulating GPUs of the chosen vendor",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyConfig(cmd, &o); err != nil {
				return err
			}
			return runCreate(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}
	cmd.Flags().StringVar(&o.configPath, "config", "", "Path to SimsConfig YAML file")
	cmd.Flags().StringVar(&o.vendor, "vendor", "", "GPU vendor to simulate (nvidia|amd)")
	cmd.Flags().StringVar(&o.name, "name", "", "Cluster name (default: sims-<vendor>)")
	cmd.Flags().IntVar(&o.workers, "workers", 2, "Number of worker nodes")
	cmd.Flags().IntVar(&o.gpusPerWorker, "gpus-per-worker", 2, "Fake GPUs advertised per worker")
	cmd.Flags().StringVar(&o.k8sVersion, "k8s-version", "v1.31.0", "Kubernetes version for kind nodes")
	cmd.Flags().BoolVar(&o.withMonitoring, "monitoring", false, "Install kube-prometheus-stack + vendor dashboard")
	cmd.Flags().BoolVar(&o.taint, "taint", false, "Add <vendor>.com/gpu=present:NoSchedule taint to worker nodes")
	return cmd
}

func applyConfig(cmd *cobra.Command, o *createOpts) error {
	if o.configPath == "" {
		if o.vendor == "" {
			return fmt.Errorf("either --vendor or --config is required")
		}
		return nil
	}
	cfg, err := simsconfig.Load(o.configPath)
	if err != nil {
		return fmt.Errorf("load --config: %w", err)
	}

	if !cmd.Flags().Changed("vendor") {
		o.vendor = cfg.Vendor
	}
	if !cmd.Flags().Changed("name") && cfg.Name != "" {
		o.name = cfg.Name
	}
	if !cmd.Flags().Changed("workers") {
		o.workers = cfg.Workers
	}
	if !cmd.Flags().Changed("gpus-per-worker") {
		o.gpusPerWorker = cfg.GPU.PerWorker
	}
	if !cmd.Flags().Changed("k8s-version") && cfg.K8sVersion != "" {
		o.k8sVersion = cfg.K8sVersion
	}
	if !cmd.Flags().Changed("taint") {
		o.taint = cfg.Taint
	}
	if !cmd.Flags().Changed("monitoring") {
		o.withMonitoring = cfg.Monitoring
	}

	if fam, ok := simsconfig.Families[cfg.GPU.Family]; ok {
		o.productName = fam.ProductName
		o.gpuMemoryBytes = cfg.GPU.MemoryBytes
	}
	o.migProfile = cfg.GPU.Features.MIG
	o.partitionMode = cfg.GPU.Features.Partition.Mode
	o.partitionCount = cfg.GPU.Features.Partition.Count
	o.defaultUtilization = cfg.Workload.DefaultUtilization
	return nil
}

func runCreate(ctx context.Context, stdout io.Writer, o *createOpts) error {
	if err := validateVendor(o.vendor); err != nil {
		return err
	}

	chartRelease, valuesBuilder, gpuResource := vendorWiring(o.vendor)

	log := newStderrLogger()
	name := o.name
	if name == "" {
		name = "sims-" + o.vendor
	}

	raw, err := config.Render(config.Options{
		Vendor:     o.vendor,
		Name:       name,
		Workers:    o.workers,
		K8sVersion: o.k8sVersion,
		Taint:      o.taint,
	})
	if err != nil {
		return err
	}

	log.Info("creating kind cluster", "name", name, "workers", o.workers, "k8s", o.k8sVersion)
	provider := cluster.New(log)
	if err := provider.Create(ctx, name, raw); err != nil {
		return err
	}

	kc, err := provider.KubeConfig(ctx, name)
	if err != nil {
		return err
	}

	log.Info("ensuring chart namespace with PSA labels", "namespace", chartNamespace)
	if err := kube.EnsureNamespace(ctx, kc, chartNamespace, psaPrivilegedLabels()); err != nil {
		return err
	}

	hc, err := helm.New(kc, chartNamespace, helm.WithLogger(log))
	if err != nil {
		return err
	}
	defer func() { _ = hc.Close() }()

	chartDir := resolveChartDir(chartRelease)
	log.Info("installing chart", "chart", chartDir, "release", chartRelease, "namespace", chartNamespace)
	if err := hc.EnsureDependencies(ctx, chartDir); err != nil {
		return err
	}
	if err := hc.Install(ctx, chartRelease, chartDir, valuesBuilder(o), helm.WithoutCreateNamespace()); err != nil {
		return err
	}

	log.Info("waiting for GPU capacity on workers",
		"resource", gpuResource, "per-worker", o.gpusPerWorker, "workers", o.workers)
	wait, cancel := context.WithTimeout(ctx, capacityWaitWindow)
	defer cancel()
	if err := kube.WaitForResourceCapacity(wait, kc, gpuResource, o.gpusPerWorker, o.workers); err != nil {
		return err
	}

	if o.withMonitoring {
		if err := installMonitoring(ctx, log, kc, o.vendor); err != nil {
			return err
		}
	}

	monitoringMsg := ""
	if o.withMonitoring {
		monitoringMsg = "\nmonitoring: run `sims gpu dashboard` to open Grafana"
	}
	_, _ = fmt.Fprintf(stdout,
		"cluster %q ready — %d workers × %d %s\nkubeconfig context: kind-%s%s\n",
		name, o.workers, o.gpusPerWorker, gpuResource, name, monitoringMsg)
	return nil
}

// vendorWiring returns the helm release name, values builder, and resource
// name for the chosen vendor. The four wiring constants per vendor live at
// the top of this file.
func vendorWiring(vendor string) (release string, values func(*createOpts) map[string]any, resource string) {
	switch vendor {
	case config.VendorAMD:
		return chartReleaseAMD, buildAMDValues, gpuResourceAMD
	default:
		return chartReleaseNVIDIA, buildNVIDIAValues, gpuResourceNVIDIA
	}
}

// installMonitoring brings up sims-monitoring (kube-prometheus-stack + the
// vendor's ServiceMonitor + dashboard CM) alongside the GPU release. The
// caller is responsible for the cluster + kubeconfig; this function pre-
// creates the monitoring namespace (Helm can't own a namespace cleanly —
// see feedback-helm-namespace-ownership memory), installs the chart with
// vendor=<vendor>, then waits up to monitoringWaitWindow for the Grafana
// Deployment to become Available.
func installMonitoring(ctx context.Context, log *slog.Logger, kubeconfig []byte, vendor string) error {
	log.Info("ensuring monitoring namespace", "namespace", monitoringNamespace)
	if err := kube.EnsureNamespace(ctx, kubeconfig, monitoringNamespace, monitoringNSLabels()); err != nil {
		return err
	}

	hc, err := helm.New(kubeconfig, monitoringNamespace, helm.WithLogger(log))
	if err != nil {
		return err
	}
	defer func() { _ = hc.Close() }()

	chartDir := resolveChartDir(monitoringChartName)
	log.Info("pulling monitoring chart deps (first run is slow, OCI fetch)")
	if err := hc.EnsureDependencies(ctx, chartDir); err != nil {
		return err
	}

	log.Info("installing monitoring chart",
		"chart", chartDir, "release", monitoringRelease, "namespace", monitoringNamespace, "vendor", vendor)
	if err := hc.Install(ctx, monitoringRelease, chartDir,
		map[string]any{"vendor": vendor},
		helm.WithoutCreateNamespace(),
	); err != nil {
		return err
	}

	grafanaDeploy := monitoringRelease + grafanaDeploymentSuffix
	log.Info("waiting for Grafana to be Available", "deployment", grafanaDeploy, "timeout", monitoringWaitWindow)
	waitCtx, cancel := context.WithTimeout(ctx, monitoringWaitWindow)
	defer cancel()
	return kube.WaitForDeploymentAvailable(waitCtx, kubeconfig, monitoringNamespace, grafanaDeploy)
}

func monitoringNSLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "sims",
		"app.kubernetes.io/part-of":    "sims",
	}
}

func validateVendor(v string) error {
	switch v {
	case config.VendorNVIDIA, config.VendorAMD:
		return nil
	default:
		return fmt.Errorf("invalid --vendor %q (must be %q or %q)", v, config.VendorNVIDIA, config.VendorAMD)
	}
}

func newStderrLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func resolveChartDir(name string) string {
	if v := os.Getenv(chartDirEnvVar); v != "" {
		return filepath.Join(v, name)
	}
	return filepath.Join(defaultChartDir, name)
}

// psaPrivilegedLabels returns the Pod Security Admission labels needed for
// fake-gpu-operator's device-plugin DaemonSet, which mounts the kubelet
// device-plugin socket via hostPath (blocked by the default baseline profile).
// Also applies the standard sims/managed-by labels for namespace ownership.
func psaPrivilegedLabels() map[string]string {
	return map[string]string{
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
		"app.kubernetes.io/managed-by":       "sims",
		"app.kubernetes.io/part-of":          "sims",
	}
}

func buildNVIDIAValues(o *createOpts) map[string]any {
	vals := map[string]any{
		"gpusPerNode": o.gpusPerWorker,
	}
	pool := map[string]any{
		"gpuCount": o.gpusPerWorker,
	}
	dcgmExtras := map[string]any{}
	if o.productName != "" {
		gpuProduct := strings.ReplaceAll(o.productName, " ", "-")
		memMiB := o.gpuMemoryBytes / (1 << 20)
		vals["gpuProduct"] = gpuProduct
		vals["gpuMemory"] = memMiB
		pool["gpuProduct"] = gpuProduct
		pool["gpuMemory"] = memMiB
		dcgmExtras["gpusPerNode"] = o.gpusPerWorker
		dcgmExtras["productName"] = o.productName
	}
	if o.migProfile != "" {
		dcgmExtras["migProfile"] = o.migProfile
	}
	if len(dcgmExtras) > 0 {
		vals["fake-dcgm-extras"] = dcgmExtras
	}
	vals["fake-gpu-operator"] = map[string]any{
		"topology": map[string]any{
			"nodePools": map[string]any{
				"default": pool,
			},
		},
	}
	return vals
}

func buildAMDValues(o *createOpts) map[string]any {
	vals := map[string]any{
		"gpusPerNode": o.gpusPerWorker,
	}
	sub := map[string]any{
		"gpusPerNode": o.gpusPerWorker,
	}
	if o.productName != "" {
		vals["productName"] = o.productName
		vals["gpuMemoryBytes"] = o.gpuMemoryBytes
		sub["productName"] = o.productName
		sub["gpuMemoryBytes"] = o.gpuMemoryBytes
	}
	if o.partitionMode != "" {
		sub["computePartition"] = map[string]any{
			"mode":  o.partitionMode,
			"count": o.partitionCount,
		}
	}
	if o.defaultUtilization != "" {
		sub["defaultUtilization"] = o.defaultUtilization
	}
	vals["fake-rocm-gpu-operator"] = sub
	return vals
}
