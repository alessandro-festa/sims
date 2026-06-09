// Package deviceplugin is the device-plugin subcommand entrypoint. It
// wires the kubelet device-plugin gRPC server + the PodResources-driven
// pod annotator and runs them until the process receives SIGTERM/SIGINT.
package deviceplugin

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	dp "github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/pkg/deviceplugin"
)

// Run parses args and runs the device-plugin until ctx is cancelled.
// args excludes the subcommand token (caller already stripped
// os.Args[0:2]).
func Run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("device-plugin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	gpus := fs.Int("gpus-per-node", 2, "Number of fake GPUs to advertise for this node.")
	resource := fs.String("resource-name", dp.DefaultResourceName, "Extended resource name to register with the kubelet.")
	socketDir := fs.String("kubelet-socket-dir", dp.DefaultSocketDir, "Directory holding kubelet.sock and the plugin's listen socket.")
	podResSock := fs.String("pod-resources-socket", dp.DefaultPodResourcesSocket, "Path to kubelet PodResources API socket.")
	annotatorInterval := fs.Duration("annotator-interval", 5*time.Second, "How often to reconcile pod annotations with kubelet PodResources state.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *gpus < 0 {
		return fmt.Errorf("--gpus-per-node must be >= 0, got %d", *gpus)
	}

	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cs, csErr := newInClusterClientset()
	var annotator dp.Annotator
	if csErr != nil {
		log.Warn("kubernetes in-cluster client unavailable; pod annotations will be skipped", "err", csErr)
	} else {
		annotator = &dp.PodResourcesAnnotator{
			Client:            cs,
			ResourceName:      *resource,
			SocketPath:        *podResSock,
			ReconcileInterval: *annotatorInterval,
			Logger:            log,
		}
	}

	server := dp.New(dp.Options{
		ResourceName: *resource,
		SocketDir:    *socketDir,
		GPUCount:     *gpus,
		Annotator:    annotator,
	})

	log.Info("device-plugin starting",
		"resource", server.ResourceName(),
		"socket", server.SocketPath(),
		"gpus", *gpus)

	errCh := make(chan error, 2)
	if annotator != nil {
		go func() {
			if err := annotator.Run(ctx); err != nil {
				errCh <- fmt.Errorf("annotator: %w", err)
			}
		}()
	}
	go func() {
		if err := server.Run(ctx); err != nil {
			errCh <- fmt.Errorf("server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func newInClusterClientset() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}
