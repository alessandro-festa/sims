// Package cluster wraps sigs.k8s.io/kind/pkg/cluster for creating, deleting,
// listing, and fetching the kubeconfig of sims-managed kind clusters.
//
// The CLI uses this package instead of execing the kind binary so that errors
// surface as typed Go errors and the binary has no runtime dependency on `kind`.
// Logs from kind are forwarded to the slog.Logger passed to New.
package cluster
