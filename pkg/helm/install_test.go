package helm

import (
	"os"
	"strings"
	"testing"
)

func TestNew_EmptyKubeconfig(t *testing.T) {
	_, err := New(nil, "default")
	if err == nil {
		t.Fatal("expected error for nil kubeconfig")
	}
	if !strings.Contains(err.Error(), "empty kubeconfig") {
		t.Errorf("unexpected error: %v", err)
	}
	_, err = New([]byte{}, "default")
	if err == nil {
		t.Fatal("expected error for empty kubeconfig")
	}
}

func TestNew_EmptyNamespace(t *testing.T) {
	_, err := New([]byte("ignored"), "")
	if err == nil {
		t.Fatal("expected error for empty namespace")
	}
	if !strings.Contains(err.Error(), "empty namespace") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Minimal kubeconfig that points at an unreachable server. Enough to construct
// a Client and exercise Close; the actual API connection isn't made until an
// Install/Upgrade/Uninstall call.
const stubKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster: { server: https://127.0.0.1:65530 }
  name: stub
contexts:
- context: { cluster: stub, user: stub }
  name: stub
current-context: stub
users:
- name: stub
  user: {}
`

func TestNew_WritesTempKubeconfig(t *testing.T) {
	c, err := New([]byte(stubKubeconfig), "default")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if c.kubeconfigPath == "" {
		t.Fatal("kubeconfigPath empty after New")
	}
	data, err := os.ReadFile(c.kubeconfigPath)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(data) != stubKubeconfig {
		t.Errorf("temp kubeconfig content mismatch:\n--- got ---\n%s\n--- want ---\n%s", data, stubKubeconfig)
	}
}

func TestClose_RemovesTempFile(t *testing.T) {
	c, err := New([]byte(stubKubeconfig), "default")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := c.kubeconfigPath
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected temp file removed, stat err=%v", err)
	}
	// Idempotent.
	if err := c.Close(); err != nil {
		t.Errorf("second Close returned %v, want nil", err)
	}
}

func TestNew_RegistryClientInitialized(t *testing.T) {
	c, err := New([]byte(stubKubeconfig), "default")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if c.registry == nil {
		t.Error("registry client not initialized; OCI charts would fail")
	}
}
