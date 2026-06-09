// Package controller is the controller subcommand entrypoint. It builds
// a controller-runtime Manager + wires the DeviceConfig reconciler.
package controller

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	amdv1alpha1 "github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/api/v1alpha1"
	rctrl "github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/controller"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(amdv1alpha1.AddToScheme(scheme))
}

// Run parses args and starts the controller until ctx is cancelled.
func Run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("controller", flag.ContinueOnError)
	fs.SetOutput(stderr)
	namespace := fs.String("namespace", "gpu-operator", "Namespace where reconciled child workloads live.")
	image := fs.String("image", "localhost:5001/fake-rocm-gpu-operator:dev", "Image used for every child workload (all are subcommands of this same image).")
	pullPolicy := fs.String("image-pull-policy", "IfNotPresent", "imagePullPolicy applied to every child workload.")
	gpus := fs.Int("gpus-per-node", 2, "Forwarded to device-plugin + metrics-exporter via --gpus-per-node.")
	product := fs.String("product-name", "MI300X", "Forwarded to metrics-exporter (--product-name) + node-labeller.")
	memBytes := fs.Int64("gpu-memory-bytes", 206158430208, "Forwarded to metrics-exporter via --memory-bytes.")
	resourceName := fs.String("resource-name", "amd.com/gpu", "Forwarded to device-plugin via --resource-name.")
	nodeSelector := fs.String("default-node-selector", "sims.io/gpu-vendor=amd", "Default nodeSelector applied to child workloads when a DeviceConfig spec doesn't set one. Format: k1=v1,k2=v2.")
	metricsAddr := fs.String("metrics-bind-address", ":8080", "Address the controller-runtime metrics endpoint binds to.")
	probeAddr := fs.String("health-probe-bind-address", ":8081", "Address the health/readiness probes bind to.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	defaultNodeSelector, err := parseNodeSelector(*nodeSelector)
	if err != nil {
		return fmt.Errorf("--default-node-selector: %w", err)
	}

	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: *probeAddr,
	})
	if err != nil {
		return fmt.Errorf("build manager: %w", err)
	}
	_ = *metricsAddr // metrics endpoint is configured via controller-runtime defaults; flag kept for forward-compat.

	reconciler := &rctrl.DeviceConfigReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Cfg: rctrl.Config{
			Image:               *image,
			ImagePullPolicy:     corev1.PullPolicy(*pullPolicy),
			GPUsPerNode:         int32(*gpus), //nolint:gosec // configured via flag, bounded by Kubernetes node-capacity int range
			ProductName:         *product,
			GPUMemoryBytes:      *memBytes,
			ResourceName:        *resourceName,
			DefaultNodeSelector: defaultNodeSelector,
			Namespace:           *namespace,
		},
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup reconciler: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", func(_ *http.Request) error { return nil }); err != nil {
		return fmt.Errorf("add healthz: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", func(_ *http.Request) error { return nil }); err != nil {
		return fmt.Errorf("add readyz: %w", err)
	}

	log.Info("controller starting",
		"namespace", *namespace,
		"image", *image,
		"gpus-per-node", *gpus,
		"resource", *resourceName)

	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("manager: %w", err)
	}
	return nil
}

// parseNodeSelector turns "k1=v1,k2=v2" into a map. Empty input → nil
// (the chart's value can be empty to signal "no default").
func parseNodeSelector(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	out := map[string]string{}
	for pair := range strings.SplitSeq(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) != 2 || kv[0] == "" {
			return nil, fmt.Errorf("invalid k=v pair %q", pair)
		}
		out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return out, nil
}
