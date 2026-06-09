package deviceplugin

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// register dials the kubelet registration socket and tells it about our
// plugin. The kubelet then connects back to our socket for ListAndWatch
// and Allocate calls. Re-registration on kubelet restart is not handled
// here — Phase 4 starts with the basic registration; if it becomes a
// flakiness source we can add a socket-watch loop later.
func (s *Server) register(ctx context.Context) error {
	kubeletSock := filepath.Join(s.socketDir, KubeletSocketName)

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(
		"unix://"+kubeletSock,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial kubelet socket %s: %w", kubeletSock, err)
	}
	defer func() { _ = conn.Close() }()

	client := pluginapi.NewRegistrationClient(conn)
	_, err = client.Register(dialCtx, &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     PluginSocketName,
		ResourceName: s.resourceName,
		Options:      &pluginapi.DevicePluginOptions{},
	})
	if err != nil {
		return fmt.Errorf("Register RPC: %w", err)
	}
	return nil
}

