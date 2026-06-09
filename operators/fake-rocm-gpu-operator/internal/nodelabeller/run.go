// Package nodelabeller is the node-labeller subcommand entrypoint. It
// runs as a DaemonSet (one pod per GPU node), patches the node's
// labels with the AMD product/CU/SIMD/family/VRAM metadata mirrored
// from the real ROCm/gpu-operator, then blocks on ctx until SIGTERM —
// kubelet keeps the pod Running so the DS reports Healthy.
package nodelabeller

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// labelKeys grouped here so the chart and tests can reference the same
// strings.
const (
	LabelGPUPresent = "feature.node.kubernetes.io/amd-gpu"
	LabelProduct    = "amd.com/gpu.product-name"
	LabelCUCount    = "amd.com/gpu.cu-count"
	LabelSIMDCount  = "amd.com/gpu.simd-count"
	LabelDeviceID   = "amd.com/gpu.device-id"
	LabelFamily     = "amd.com/gpu.family"
	LabelVRAM       = "amd.com/gpu.vram"
)

// Run parses args, patches the node's labels, and blocks until ctx is
// cancelled.
func Run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("node-labeller", flag.ContinueOnError)
	fs.SetOutput(stderr)
	product := fs.String("product-name", "MI300X", "Card product name.")
	cuCount := fs.Int("cu-count", 304, "Compute unit count.")
	simdCount := fs.Int("simd-count", 1216, "SIMD count.")
	deviceID := fs.String("device-id", "0x74a1", "PCI device ID.")
	family := fs.String("family", "MI300", "GPU family.")
	vram := fs.String("vram", "192GB", "Total VRAM size (human-readable).")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		return errors.New("NODE_NAME env required (set via downward API)")
	}

	cs, err := newInClusterClientset()
	if err != nil {
		return fmt.Errorf("in-cluster client: %w", err)
	}

	labels := map[string]string{
		LabelGPUPresent: "true",
		LabelProduct:    *product,
		LabelCUCount:    strconv.Itoa(*cuCount),
		LabelSIMDCount:  strconv.Itoa(*simdCount),
		LabelDeviceID:   *deviceID,
		LabelFamily:     *family,
		LabelVRAM:       *vram,
	}

	if err := patchNodeLabels(ctx, cs, nodeName, labels); err != nil {
		return fmt.Errorf("patch node: %w", err)
	}
	log.Info("node labels applied; sleeping until SIGTERM",
		"node", nodeName,
		"product", *product,
		"cu_count", *cuCount,
		"family", *family)

	<-ctx.Done()
	return nil
}

// patchNodeLabels does a JSON merge patch on the node's metadata.labels.
// Idempotent: setting a label to its existing value is a no-op on the
// server side.
func patchNodeLabels(ctx context.Context, cs kubernetes.Interface, name string, labels map[string]string) error {
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"labels": labels,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}
	_, err = cs.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func newInClusterClientset() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}
