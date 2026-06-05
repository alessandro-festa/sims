// Package cluster wraps sigs.k8s.io/kind/pkg/cluster for creating, deleting,
// and inspecting kind clusters from the sims CLI.
//
// Phase 1 will implement Create/Delete using a templated kind config from
// pkg/config and a local image registry container.
package cluster
