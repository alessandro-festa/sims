package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
)

// severity controls whether a failing check fails the whole command and
// how the row is rendered.
type severity int

const (
	sevCritical severity = iota // failing critical → exit 1
	sevWarn                     // failing warn → printed but exit 0
	sevInfo                     // never "fails"; just surfaces state
)

func (s severity) label() string {
	switch s {
	case sevCritical:
		return "critical"
	case sevWarn:
		return "warn"
	case sevInfo:
		return "info"
	}
	return "?"
}

// check is a single environment probe. result is "ok" / "fail" / "info".
// hint is a one-line remediation when failing; detail is optional supporting
// text printed in the same row (e.g. a discovered value).
type check struct {
	name     string
	severity severity
	run      func(ctx context.Context) (ok bool, hint string, detail string)
}

func newGPUDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run pre-flight environment checks (Docker, registry, network)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.Context(), cmd.OutOrStdout())
		},
	}
	return cmd
}

func runDoctor(ctx context.Context, stdout io.Writer) error {
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "CHECK\tSEVERITY\tRESULT\tDETAIL"); err != nil {
		return err
	}

	criticalFailed := false
	for _, c := range defaultChecks() {
		ok, hint, detail := c.run(ctx)
		result := "ok"
		if !ok {
			result = "fail"
			if c.severity == sevCritical {
				criticalFailed = true
			}
		}
		if c.severity == sevInfo {
			result = "info"
		}
		row := detail
		if !ok && hint != "" {
			if row != "" {
				row += " — "
			}
			row += hint
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.name, c.severity.label(), result, row); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if criticalFailed {
		return errors.New("one or more critical checks failed")
	}
	return nil
}

func defaultChecks() []check {
	return []check{
		{name: "docker daemon", severity: sevCritical, run: checkDockerDaemon},
		{name: "docker insecure-registries (localhost:5001)", severity: sevCritical, run: checkInsecureRegistry},
		{name: "kind-registry container", severity: sevWarn, run: checkRegistryContainer},
		{name: "sims clusters", severity: sevInfo, run: checkSimsClusters},
		{name: "ghcr.io reachable", severity: sevWarn, run: checkGHCR},
	}
}

func checkDockerDaemon(ctx context.Context) (bool, string, string) {
	out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return false, "start Docker Desktop, or `systemctl start docker` on Linux", ""
	}
	return true, "", "server " + strings.TrimSpace(string(out))
}

func checkInsecureRegistry(ctx context.Context) (bool, string, string) {
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{json .RegistryConfig}}").Output()
	if err != nil {
		return false, "Docker daemon not reachable — see the previous check", ""
	}
	text := string(out)
	for _, needle := range []string{"localhost:5001", "127.0.0.1:5001"} {
		if strings.Contains(text, needle) {
			return true, "", "found " + needle
		}
	}
	return false, `Docker Desktop → Settings → Docker Engine → add "insecure-registries": ["localhost:5001"] and Apply & restart`, ""
}

func checkRegistryContainer(ctx context.Context) (bool, string, string) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", cluster.DefaultRegistryName).Output()
	if err != nil {
		return false, "first `sims gpu create` will start it", "not present"
	}
	if strings.TrimSpace(string(out)) != "true" {
		return false, "container exists but is stopped; `docker start " + cluster.DefaultRegistryName + "`", "stopped"
	}
	return true, "", "running"
}

func checkSimsClusters(ctx context.Context) (bool, string, string) {
	provider := cluster.New(nil)
	all, err := provider.List(ctx)
	if err != nil {
		return false, "kind not reachable", err.Error()
	}
	sims := filterSimsClusters(all)
	if len(sims) == 0 {
		return true, "", "(none)"
	}
	return true, "", strings.Join(sims, ", ")
}

func checkGHCR(ctx context.Context) (bool, string, string) {
	httpCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(httpCtx, http.MethodGet, "https://ghcr.io/v2/", nil)
	if err != nil {
		return false, "build HTTP request", err.Error()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, "check network / proxy; `sims gpu create` pulls fake-gpu-operator from OCI", err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	// GHCR returns 401 (unauthenticated) for an anonymous /v2/ request; any
	// 2xx or 401 means we're talking to GHCR.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return true, "", fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return false, "unexpected status from ghcr.io", fmt.Sprintf("HTTP %d", resp.StatusCode)
}
