package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/alessandro-festa/sims/pkg/cluster"
)

const (
	grafanaService    = "sims-monitoring-grafana"
	grafanaLocalPort  = 3000
	grafanaRemotePort = 80
	grafanaURL        = "http://localhost:3000"
	grafanaUser       = "admin"
	grafanaPassword   = "prom-operator"
)

type dashboardOpts struct {
	name string
	open bool
	stop bool
}

func newGPUDashboardCmd() *cobra.Command {
	var o dashboardOpts
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Start (or stop) a background port-forward to Grafana on :3000",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDashboard(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}
	cmd.Flags().StringVar(&o.name, "name", "", "Cluster name (default: the only sims-* cluster)")
	cmd.Flags().BoolVar(&o.open, "open", false, "Open Grafana in the default browser after starting the forward")
	cmd.Flags().BoolVar(&o.stop, "stop", false, "Stop the running port-forward instead of starting one")
	return cmd
}

func runDashboard(ctx context.Context, stdout io.Writer, o *dashboardOpts) error {
	provider := cluster.New(newStderrLogger())
	resolved, err := resolveClusterName(ctx, provider, o.name)
	if err != nil {
		return err
	}

	pidPath, err := grafanaPIDPath(resolved)
	if err != nil {
		return err
	}

	if o.stop {
		return stopForward(stdout, pidPath)
	}

	pid, running := readRunningPID(pidPath)
	if running {
		_, _ = fmt.Fprintf(stdout, "Grafana port-forward already running on %s (pid=%d)\n", grafanaURL, pid)
		if o.open {
			return openBrowser(grafanaURL)
		}
		return nil
	}

	startedPID, err := startForward(resolved, pidPath)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Grafana available at %s (user=%s, pass=%s, pid=%d)\nstop with: sims gpu dashboard --stop\n",
		grafanaURL, grafanaUser, grafanaPassword, startedPID)

	if o.open {
		// Give kubectl a moment to bind the port before opening the browser.
		time.Sleep(500 * time.Millisecond)
		return openBrowser(grafanaURL)
	}
	return nil
}

// resolveClusterName picks the single sims-* cluster when name is empty,
// errors on 0 or >1. Validates name exists when provided. Cluster-name-only
// variant of resolveClusterAndKubeconfig (which also fetches kubeconfig);
// dashboard doesn't need the kubeconfig because kubectl reads it itself.
func resolveClusterName(ctx context.Context, provider *cluster.Provider, name string) (string, error) {
	all, err := provider.List(ctx)
	if err != nil {
		return "", err
	}
	if name == "" {
		sims := filterSimsClusters(all)
		switch len(sims) {
		case 0:
			return "", errors.New("no sims-managed cluster found on this host")
		case 1:
			return sims[0], nil
		default:
			return "", fmt.Errorf("multiple sims clusters found %v; pass --name", sims)
		}
	}
	if !slices.Contains(all, name) {
		return "", fmt.Errorf("cluster %q not found", name)
	}
	return name, nil
}

func grafanaPIDPath(clusterName string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	dir := filepath.Join(cache, "sims", clusterName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return filepath.Join(dir, "grafana.pid"), nil
}

func readRunningPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	// signal 0 = "is the process reachable" without actually signalling.
	if err := syscall.Kill(pid, 0); err != nil {
		return pid, false
	}
	return pid, true
}

func startForward(clusterName, pidPath string) (int, error) {
	args := []string{
		"--context", "kind-" + clusterName,
		"port-forward",
		"-n", monitoringNamespace,
		"svc/" + grafanaService,
		fmt.Sprintf("%d:%d", grafanaLocalPort, grafanaRemotePort),
	}
	cmd := exec.Command("kubectl", args...)
	// Detach: own process group so the parent exiting doesn't propagate
	// SIGHUP; drop stdio so kubectl's logs don't bleed onto the terminal
	// after the parent returns.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start kubectl port-forward: %w", err)
	}
	// Capture PID BEFORE Release — Process.Pid returns -1 after Release().
	pid := cmd.Process.Pid
	// Release the child so it doesn't become a zombie when we exit.
	if err := cmd.Process.Release(); err != nil {
		return 0, fmt.Errorf("release kubectl process: %w", err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", pidPath, err)
	}
	return pid, nil
}

func stopForward(stdout io.Writer, pidPath string) error {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			_, _ = fmt.Fprintln(stdout, "no port-forward PID file found; nothing to stop")
			return nil
		}
		return fmt.Errorf("read %s: %w", pidPath, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		// Stale or corrupt — remove and report success.
		_ = os.Remove(pidPath)
		_, _ = fmt.Fprintln(stdout, "PID file was unusable; removed")
		return nil
	}
	// SIGTERM; ignore "not found" since the child may have already died.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill pid %d: %w", pid, err)
	}
	_ = os.Remove(pidPath)
	_, _ = fmt.Fprintf(stdout, "Grafana port-forward stopped (pid=%d)\n", pid)
	return nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("--open not supported on %s; visit %s manually", runtime.GOOS, url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return cmd.Process.Release()
}
