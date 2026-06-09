package deviceplugin

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// shortTempDir creates a temp dir under /tmp so unix-socket paths stay
// under macOS's 104-byte limit (Go's t.TempDir() roots under
// /var/folders/... which easily exceeds it).
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sims-dp-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// fakeRegistrationServer captures Register calls without dispatching them
// anywhere. Stands in for the kubelet's registration socket.
type fakeRegistrationServer struct {
	pluginapi.UnimplementedRegistrationServer
	mu       sync.Mutex
	requests []*pluginapi.RegisterRequest
}

func (f *fakeRegistrationServer) Register(_ context.Context, r *pluginapi.RegisterRequest) (*pluginapi.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r)
	return &pluginapi.Empty{}, nil
}

func (f *fakeRegistrationServer) lastRequest() *pluginapi.RegisterRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return nil
	}
	return f.requests[len(f.requests)-1]
}

// startFakeKubelet brings up a fake kubelet registration server on
// $dir/kubelet.sock. Returns the server (for assertions) and a stop func.
func startFakeKubelet(t *testing.T, dir string) (*fakeRegistrationServer, func()) {
	t.Helper()
	sockPath := filepath.Join(dir, KubeletSocketName)
	lis, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen kubelet sock: %v", err)
	}
	gs := grpc.NewServer()
	frs := &fakeRegistrationServer{}
	pluginapi.RegisterRegistrationServer(gs, frs)
	go func() { _ = gs.Serve(lis) }()
	return frs, func() {
		gs.GracefulStop()
		_ = lis.Close()
	}
}

func TestServer_RegistersAndServes(t *testing.T) {
	dir := shortTempDir(t)
	frs, stopKubelet := startFakeKubelet(t, dir)
	defer stopKubelet()

	s := New(Options{
		ResourceName: "amd.com/gpu",
		SocketDir:    dir,
		GPUCount:     3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Allow time for socket creation + Register call.
	if err := waitForRegister(frs, 2*time.Second); err != nil {
		t.Fatalf("kubelet never received Register call: %v", err)
	}

	req := frs.lastRequest()
	if req == nil {
		t.Fatal("no Register request captured")
	}
	if req.GetResourceName() != "amd.com/gpu" {
		t.Errorf("Register.ResourceName = %q, want amd.com/gpu", req.GetResourceName())
	}
	if req.GetEndpoint() != PluginSocketName {
		t.Errorf("Register.Endpoint = %q, want %q", req.GetEndpoint(), PluginSocketName)
	}
	if req.GetVersion() != pluginapi.Version {
		t.Errorf("Register.Version = %q, want %q", req.GetVersion(), pluginapi.Version)
	}

	// Talk to the plugin via its own socket.
	conn, err := grpc.NewClient(
		"unix://"+s.SocketPath(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial plugin socket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := pluginapi.NewDevicePluginClient(conn)

	// ListAndWatch streams; assert the first send contains 3 devices.
	streamCtx, streamCancel := context.WithTimeout(ctx, 3*time.Second)
	defer streamCancel()
	stream, err := client.ListAndWatch(streamCtx, &pluginapi.Empty{})
	if err != nil {
		t.Fatalf("ListAndWatch start: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("ListAndWatch Recv: %v", err)
	}
	if len(resp.GetDevices()) != 3 {
		t.Errorf("device count = %d, want 3", len(resp.GetDevices()))
	}
	for i, d := range resp.GetDevices() {
		wantID := "gpu-" + string(rune('0'+i))
		if d.GetID() != wantID {
			t.Errorf("devices[%d].ID = %q, want %q", i, d.GetID(), wantID)
		}
		if d.GetHealth() != pluginapi.Healthy {
			t.Errorf("devices[%d].Health = %q, want Healthy", i, d.GetHealth())
		}
	}

	// Allocate returns empty responses, one per container request.
	aResp, err := client.Allocate(ctx, &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIds: []string{"gpu-0"}},
			{DevicesIds: []string{"gpu-1", "gpu-2"}},
		},
	})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if len(aResp.GetContainerResponses()) != 2 {
		t.Errorf("container responses = %d, want 2", len(aResp.GetContainerResponses()))
	}
	for i, c := range aResp.GetContainerResponses() {
		if len(c.GetDevices()) != 0 || len(c.GetMounts()) != 0 || len(c.GetEnvs()) != 0 {
			t.Errorf("container response %d non-empty: %+v", i, c)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("server.Run returned err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("server.Run did not return after ctx cancel")
	}
}

func TestServer_BuildDevices(t *testing.T) {
	devs := buildDevices(4)
	if len(devs) != 4 {
		t.Fatalf("len = %d, want 4", len(devs))
	}
	for i, d := range devs {
		want := "gpu-" + string(rune('0'+i))
		if d.GetID() != want {
			t.Errorf("devs[%d].ID = %q, want %q", i, d.GetID(), want)
		}
	}
	if devs := buildDevices(0); len(devs) != 0 {
		t.Errorf("zero count produced %d devices", len(devs))
	}
}

func waitForRegister(frs *fakeRegistrationServer, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if frs.lastRequest() != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return context.DeadlineExceeded
}
