package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
)

// inClusterRegistry is the registry hostname kind nodes resolve via the
// containerd mirror configured by pkg/config. It also happens to be the
// host-side port-forward, so the same string works from outside the cluster.
const inClusterRegistry = "localhost:5001"

func newGPULoadImageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "load-image <image>",
		Short: "Load a local Docker image into the kind cluster's local registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLoadImage(cmd.Context(), cmd.OutOrStdout(), args[0])
		},
	}
	return cmd
}

func runLoadImage(ctx context.Context, stdout io.Writer, image string) error {
	if image == "" {
		return errors.New("image argument is required")
	}

	log := newStderrLogger()
	provider := cluster.New(log)
	all, err := provider.List(ctx)
	if err != nil {
		return err
	}
	if len(filterSimsClusters(all)) == 0 {
		return errors.New("no sims-managed cluster is running; run `sims gpu create` first " +
			"so the local registry is reachable")
	}

	target := inClusterRegistry + "/" + image

	log.Info("tagging image", "src", image, "dst", target)
	if err := runDocker(ctx, "tag", image, target); err != nil {
		return err
	}

	log.Info("pushing image to registry", "image", target)
	if err := runDocker(ctx, "push", target); err != nil {
		if strings.Contains(err.Error(), "server gave HTTP response to HTTPS client") {
			log.Error("The local registry serves plain HTTP. Add it to your Docker daemon's insecure-registries:")
			log.Error("  Docker Desktop: Settings → Docker Engine → add \"insecure-registries\": [\"localhost:5001\"], Apply & restart")
			log.Error("  Linux Docker: edit /etc/docker/daemon.json to add the same key, then systemctl restart docker")
		}
		return err
	}

	_, _ = fmt.Fprintf(stdout, "image loaded; reference it from cluster manifests as:\n  %s\n", target)
	return nil
}

func runDocker(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
