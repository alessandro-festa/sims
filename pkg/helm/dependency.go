package helm

import (
	"context"
	"fmt"
	"io"

	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
)

// EnsureDependencies materializes the chart's declared dependencies into
// chartPath/charts/ using helm's downloader.Manager — the same code path as
// `helm dependency build`. The chart's Chart.lock pins exact versions and is
// honored for reproducibility.
//
// The context is accepted for API symmetry with Install/Upgrade; helm's
// Manager.Build does not currently take a context, so cancellation only
// applies before the call.
func (c *Client) EnsureDependencies(ctx context.Context, chartPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	man := &downloader.Manager{
		Out:              io.Discard,
		ChartPath:        chartPath,
		SkipUpdate:       true,
		Getters:          getter.All(c.settings),
		RegistryClient:   c.registry,
		RepositoryConfig: c.settings.RepositoryConfig,
		RepositoryCache:  c.settings.RepositoryCache,
	}
	if err := man.Build(); err != nil {
		return fmt.Errorf("helm dependency build %q: %w", chartPath, err)
	}
	return nil
}
