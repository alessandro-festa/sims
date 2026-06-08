package cli

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestRunLoadImage_EmptyImage(t *testing.T) {
	err := runLoadImage(context.Background(), io.Discard, "")
	if err == nil || !strings.Contains(err.Error(), "image argument is required") {
		t.Fatalf("expected empty-image error, got: %v", err)
	}
}

// The richer paths — no-sims-cluster, docker exec failures — depend on docker
// being available and a kind cluster's state. They're covered by the live
// smoke in the PR description and (eventually) by the e2e test in #10.
