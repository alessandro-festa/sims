package cluster

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNoSuchContainer(t *testing.T) {
	for _, in := range []string{
		"Error: No such container: kind-registry",
		"Error: No such object: kind-registry",
		"error response from daemon: no such container: foo",
	} {
		if !noSuchContainer(in) {
			t.Errorf("noSuchContainer(%q) = false, want true", in)
		}
	}
	for _, in := range []string{
		"",
		"network connect failed",
		"random unrelated error",
	} {
		if noSuchContainer(in) {
			t.Errorf("noSuchContainer(%q) = true, want false", in)
		}
	}
}

func TestAlreadyConnected(t *testing.T) {
	for _, in := range []string{
		"Error response from daemon: endpoint with name kind-registry already exists in network kind",
		"already attached to network kind",
	} {
		if !alreadyConnected(in) {
			t.Errorf("alreadyConnected(%q) = false, want true", in)
		}
	}
	if alreadyConnected("network not found") {
		t.Error("alreadyConnected(false case) = true")
	}
}

// TestRegistry_Smoke exercises the full lifecycle against real Docker:
// Ensure (cold) → assert running → Ensure (warm, idempotent) →
// MaybeStopRegistry with no sims clusters → assert removed.
// Gated by KIND_E2E=1 because it mutates the local Docker state.
func TestRegistry_Smoke(t *testing.T) {
	if os.Getenv("KIND_E2E") != "1" {
		t.Skip("KIND_E2E=1 required to run this test (mutates local Docker state)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Clean baseline regardless of how the previous run exited.
	_ = removeRegistryContainer(ctx)

	if err := EnsureRegistry(ctx); err != nil {
		t.Fatalf("EnsureRegistry (cold): %v", err)
	}
	if running, err := containerRunning(ctx, DefaultRegistryName); err != nil || !running {
		t.Fatalf("after cold Ensure, container running=%v err=%v", running, err)
	}

	// Warm path — must not error or duplicate.
	if err := EnsureRegistry(ctx); err != nil {
		t.Fatalf("EnsureRegistry (warm): %v", err)
	}

	// No sims clusters → registry should be removed.
	if err := MaybeStopRegistry(ctx); err != nil {
		t.Fatalf("MaybeStopRegistry: %v", err)
	}
	if running, err := containerRunning(ctx, DefaultRegistryName); err != nil || running {
		t.Fatalf("after MaybeStop, container running=%v err=%v (want running=false)", running, err)
	}
}
