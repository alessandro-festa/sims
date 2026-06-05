package cluster

import (
	"context"
	"fmt"
	"log/slog"

	"sigs.k8s.io/kind/pkg/cluster"
	kindlog "sigs.k8s.io/kind/pkg/log"
)

// Provider wraps sigs.k8s.io/kind/pkg/cluster.Provider for the subset of
// operations the sims CLI uses: Create, Delete, List, KubeConfig.
type Provider struct {
	p *cluster.Provider
}

// New returns a Provider that forwards kind's log output to the given slog.Logger.
// If logger is nil, slog.Default() is used.
func New(logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		p: cluster.NewProvider(
			cluster.ProviderWithLogger(&slogAdapter{logger: logger}),
		),
	}
}

// Create brings up a kind cluster named `name` from the rendered kind config
// in `rawConfig` (typically produced by pkg/config.Render). Returns an error
// if a cluster of that name already exists.
//
// kind's Create is synchronous and does not accept a context; the ctx argument
// is checked once before the call so a cancelled context fails fast.
func (p *Provider) Create(ctx context.Context, name string, rawConfig []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.p.Create(name, cluster.CreateWithRawConfig(rawConfig)); err != nil {
		return fmt.Errorf("kind create %q: %w", name, err)
	}
	return nil
}

// Delete removes the named cluster. kind's Delete is idempotent — deleting a
// non-existent cluster returns nil.
func (p *Provider) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.p.Delete(name, "" /* let kind resolve the kubeconfig path */); err != nil {
		return fmt.Errorf("kind delete %q: %w", name, err)
	}
	return nil
}

// List returns the names of all kind clusters visible on this host (not just
// sims-managed ones).
func (p *Provider) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := p.p.List()
	if err != nil {
		return nil, fmt.Errorf("kind list: %w", err)
	}
	return names, nil
}

// KubeConfig returns the kubeconfig YAML for the named cluster, using the
// cluster's external (host-reachable) endpoint.
func (p *Provider) KubeConfig(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	kc, err := p.p.KubeConfig(name, false /* external endpoint */)
	if err != nil {
		return nil, fmt.Errorf("kind kubeconfig %q: %w", name, err)
	}
	return []byte(kc), nil
}

// slogAdapter implements sigs.k8s.io/kind/pkg/log.Logger by forwarding to slog.
type slogAdapter struct {
	logger *slog.Logger
}

func (s *slogAdapter) Warn(message string) { s.logger.Warn(message) }

func (s *slogAdapter) Warnf(format string, args ...any) {
	s.logger.Warn(fmt.Sprintf(format, args...))
}

func (s *slogAdapter) Error(message string) { s.logger.Error(message) }

func (s *slogAdapter) Errorf(format string, args ...any) {
	s.logger.Error(fmt.Sprintf(format, args...))
}

// V routes kind's verbosity levels into slog levels: V(0) -> Info,
// V(1)+ -> Debug. Higher levels remain enabled (slog handlers decide).
func (s *slogAdapter) V(level kindlog.Level) kindlog.InfoLogger {
	return &slogInfoLogger{logger: s.logger, level: level}
}

type slogInfoLogger struct {
	logger *slog.Logger
	level  kindlog.Level
}

func (s *slogInfoLogger) Info(message string) {
	if s.level == 0 {
		s.logger.Info(message)
	} else {
		s.logger.Debug(message)
	}
}

func (s *slogInfoLogger) Infof(format string, args ...any) {
	s.Info(fmt.Sprintf(format, args...))
}

func (s *slogInfoLogger) Enabled() bool {
	if s.level == 0 {
		return s.logger.Enabled(context.Background(), slog.LevelInfo)
	}
	return s.logger.Enabled(context.Background(), slog.LevelDebug)
}
