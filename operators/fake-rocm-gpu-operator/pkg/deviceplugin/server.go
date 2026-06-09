// Package deviceplugin implements a kubelet device-plugin server
// (k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1) advertising fake
// amd.com/gpu devices. It runs as a DaemonSet pod on every GPU node and:
//
//  1. Registers with the kubelet via /var/lib/kubelet/device-plugins/kubelet.sock.
//  2. Serves ListAndWatch with N fake Healthy devices.
//  3. Responds to Allocate with empty mounts/env (no real device files).
//
// Pod annotation (sims.io/assigned-gpus) is handled by the separate
// Annotator goroutine which polls the kubelet PodResources API — see
// annotate.go.
package deviceplugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	// DefaultResourceName is the extended resource sims advertises for
	// AMD GPUs. Matches the upstream ROCm/k8s-device-plugin convention.
	DefaultResourceName = "amd.com/gpu"

	// DefaultSocketDir is the canonical kubelet plugin socket directory
	// mounted into the plugin pod via hostPath.
	DefaultSocketDir = "/var/lib/kubelet/device-plugins"

	// PluginSocketName is the basename of the plugin's socket. Kubelet
	// resolves it under DefaultSocketDir during Register.
	PluginSocketName = "amd-gpu.sock"

	// KubeletSocketName is the basename of the kubelet's registration
	// socket inside DefaultSocketDir.
	KubeletSocketName = "kubelet.sock"

	// listAndWatchInterval is how often the plugin re-sends the device
	// list to the kubelet. The list is unchanged for fake GPUs; the
	// re-send doubles as a keepalive.
	listAndWatchInterval = 30 * time.Second
)

// Options configures a Server. All fields are optional; zero values fall
// back to the Default* constants and the package's idle behavior.
type Options struct {
	ResourceName string
	SocketDir    string
	GPUCount     int
	Annotator    Annotator
}

// Server is the device-plugin gRPC server. Use New + Run.
type Server struct {
	pluginapi.UnimplementedDevicePluginServer

	resourceName string
	socketDir    string
	gpuCount     int
	annotator    Annotator

	devices []*pluginapi.Device

	mu          sync.Mutex
	grpcServer  *grpc.Server
	listener    net.Listener
	stopUpdates chan struct{}
}

// New builds a Server ready for Run. Falls back to Default* constants for
// any unset Options field. A nil Annotator is fine — the server just won't
// surface assigned-gpus annotations (useful for unit tests).
func New(opts Options) *Server {
	s := &Server{
		resourceName: opts.ResourceName,
		socketDir:    opts.SocketDir,
		gpuCount:     opts.GPUCount,
		annotator:    opts.Annotator,
		stopUpdates:  make(chan struct{}),
	}
	if s.resourceName == "" {
		s.resourceName = DefaultResourceName
	}
	if s.socketDir == "" {
		s.socketDir = DefaultSocketDir
	}
	if s.gpuCount < 0 {
		s.gpuCount = 0
	}
	s.devices = buildDevices(s.gpuCount)
	return s
}

// ResourceName returns the resource string the server advertises. Useful
// for tests asserting registration parameters.
func (s *Server) ResourceName() string { return s.resourceName }

// Devices returns a snapshot of the fake device list the server emits via
// ListAndWatch. The slice is independent of the server's internal copy.
func (s *Server) Devices() []*pluginapi.Device {
	out := make([]*pluginapi.Device, len(s.devices))
	copy(out, s.devices)
	return out
}

// SocketPath returns the absolute path of the plugin's listening socket.
func (s *Server) SocketPath() string {
	return filepath.Join(s.socketDir, PluginSocketName)
}

// Run starts the gRPC server, registers with the kubelet, and blocks
// until ctx is cancelled or the underlying listener fails. The kubelet
// socket directory must already exist (the chart's hostPath mount handles
// that in cluster).
func (s *Server) Run(ctx context.Context) error {
	if err := s.start(ctx); err != nil {
		return err
	}
	if err := s.register(ctx); err != nil {
		s.stop()
		return fmt.Errorf("register with kubelet: %w", err)
	}
	<-ctx.Done()
	s.stop()
	return nil
}

func (s *Server) start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sockPath := s.SocketPath()
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket %s: %w", sockPath, err)
	}

	lis, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", sockPath, err)
	}
	s.listener = lis

	s.grpcServer = grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(s.grpcServer, s)

	go func() {
		_ = s.grpcServer.Serve(lis)
	}()
	return nil
}

func (s *Server) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case <-s.stopUpdates:
		// already closed
	default:
		close(s.stopUpdates)
	}
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
		s.grpcServer = nil
	}
	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}
	if path := s.SocketPath(); path != "" {
		_ = os.Remove(path)
	}
}

// GetDevicePluginOptions reports server capabilities. We don't need
// PreStartContainer or GetPreferredAllocation — kubelet's default
// allocation strategy is fine for fake devices.
func (s *Server) GetDevicePluginOptions(_ context.Context, _ *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

// ListAndWatch streams the device list to the kubelet. The list is fixed
// for fake GPUs; re-sending on a 30s tick doubles as a keepalive that
// catches kubelet restarts via stream errors.
func (s *Server) ListAndWatch(_ *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	if err := stream.Send(&pluginapi.ListAndWatchResponse{Devices: s.Devices()}); err != nil {
		return err
	}
	ticker := time.NewTicker(listAndWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-s.stopUpdates:
			return nil
		case <-ticker.C:
			if err := stream.Send(&pluginapi.ListAndWatchResponse{Devices: s.Devices()}); err != nil {
				return err
			}
		}
	}
}

// Allocate is called by the kubelet when scheduling a pod that requested
// our resource. For fake GPUs there are no host devices to mount or env
// vars to inject — we return an empty response per container.
//
// The kubelet's Allocate request does NOT include the pod identity. The
// Annotator (annotate.go) discovers pod ↔ device assignments by polling
// the PodResources API instead.
func (s *Server) Allocate(_ context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := &pluginapi.AllocateResponse{
		ContainerResponses: make([]*pluginapi.ContainerAllocateResponse, len(req.ContainerRequests)),
	}
	for i := range req.ContainerRequests {
		resp.ContainerResponses[i] = &pluginapi.ContainerAllocateResponse{}
	}
	return resp, nil
}

// GetPreferredAllocation is unimplemented (we report it disabled via
// GetDevicePluginOptions, so kubelet won't call it — but keep a stub for
// API completeness).
func (s *Server) GetPreferredAllocation(_ context.Context, _ *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}

// PreStartContainer is unimplemented for the same reason as
// GetPreferredAllocation.
func (s *Server) PreStartContainer(_ context.Context, _ *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func buildDevices(n int) []*pluginapi.Device {
	devs := make([]*pluginapi.Device, n)
	for i := range n {
		devs[i] = &pluginapi.Device{
			ID:     "gpu-" + strconv.Itoa(i),
			Health: pluginapi.Healthy,
		}
	}
	return devs
}
