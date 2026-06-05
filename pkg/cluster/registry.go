package cluster

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Defaults for the local image registry container that kind nodes use as a
// mirror for `localhost:<port>`. Must match the port encoded in the
// containerdConfigPatches written by pkg/config.
const (
	DefaultRegistryName = "kind-registry"
	DefaultRegistryPort = 5001
)

const (
	registryImage   = "registry:2"
	kindNetworkName = "kind"
	simsPrefix      = "sims-"
)

// EnsureRegistry starts the local Docker registry container if it isn't
// already running. Safe to call multiple times.
//
// Call this BEFORE creating a kind cluster (since pulling the registry image
// can take a few seconds). After the cluster is up, call
// ConnectRegistryToKindNetwork to attach the container to the kind network so
// nodes can resolve `<DefaultRegistryName>:<DefaultRegistryPort>`.
func EnsureRegistry(ctx context.Context) error {
	running, err := containerRunning(ctx, DefaultRegistryName)
	if err != nil {
		return err
	}
	if running {
		return nil
	}
	port := fmt.Sprintf("%d", DefaultRegistryPort)
	cmd := exec.CommandContext(ctx, "docker", "run", "-d",
		"--restart=always",
		"--name", DefaultRegistryName,
		"-p", port+":"+port,
		"-e", "REGISTRY_HTTP_ADDR=:"+port,
		registryImage,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker run registry: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ConnectRegistryToKindNetwork attaches the registry container to the kind
// Docker network so kind nodes can reach it by container name. The kind
// network is created on the first `kind create`, so call this AFTER the
// cluster is up. Idempotent: returns nil if the container is already
// attached.
func ConnectRegistryToKindNetwork(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "connect", kindNetworkName, DefaultRegistryName)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if alreadyConnected(string(out)) {
		return nil
	}
	return fmt.Errorf("docker network connect %s %s: %w: %s",
		kindNetworkName, DefaultRegistryName, err, strings.TrimSpace(string(out)))
}

// MaybeStopRegistry removes the registry container, but only when no
// sims-managed kind clusters remain on this host. Cluster ownership is
// detected via the "sims-" name prefix; non-sims kind clusters do not
// keep the registry alive (they use their own registries, if any).
func MaybeStopRegistry(ctx context.Context) error {
	p := New(nil)
	names, err := p.List(ctx)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}
	for _, n := range names {
		if strings.HasPrefix(n, simsPrefix) {
			return nil
		}
	}
	return removeRegistryContainer(ctx)
}

func removeRegistryContainer(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", DefaultRegistryName)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if noSuchContainer(string(out)) {
		return nil
	}
	return fmt.Errorf("docker rm %s: %w: %s", DefaultRegistryName, err, strings.TrimSpace(string(out)))
}

func containerRunning(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", name)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && noSuchContainer(string(ee.Stderr)) {
			return false, nil
		}
		return false, fmt.Errorf("docker inspect %s: %w", name, err)
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

func noSuchContainer(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "no such container") || strings.Contains(s, "no such object")
}

func alreadyConnected(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "already exists") || strings.Contains(s, "already attached")
}
