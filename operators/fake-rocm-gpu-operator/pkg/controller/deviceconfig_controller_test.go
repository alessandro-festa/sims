package controller

import (
	"context"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	amdv1alpha1 "github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/api/v1alpha1"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(amdv1alpha1.AddToScheme(s))
	return s
}

func newReconciler(t *testing.T, objs ...client.Object) (*DeviceConfigReconciler, client.Client) {
	t.Helper()
	s := newScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&amdv1alpha1.DeviceConfig{}).
		WithObjects(objs...).
		Build()
	r := &DeviceConfigReconciler{
		Client: c,
		Scheme: s,
		Cfg: Config{
			Image:               "ghcr.io/alessandro-festa/fake-rocm-gpu-operator:0.1.0",
			ImagePullPolicy:     corev1.PullIfNotPresent,
			GPUsPerNode:         2,
			ProductName:         "MI300X",
			GPUMemoryBytes:      206158430208,
			ResourceName:        "amd.com/gpu",
			DefaultNodeSelector: map[string]string{"sims.io/gpu-vendor": "amd"},
			Namespace:           "gpu-operator",
		},
	}
	return r, c
}

// TestReconcile_AcceptsROCmStyleManifest is the gist of #47: a real
// (lightly trimmed) ROCm/gpu-operator DeviceConfig must apply cleanly,
// and the reconciler must produce the expected child workloads.
func TestReconcile_AcceptsROCmStyleManifest(t *testing.T) {
	cr := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: amdv1alpha1.DeviceConfigSpec{
			Driver: amdv1alpha1.DriverSpec{Enable: ptrBool(true)}, // accepted, ignored
			DevicePlugin: amdv1alpha1.DevicePluginSpec{
				Enable:          ptrBool(true),
				ImagePullPolicy: "IfNotPresent",
			},
			MetricsExporter: amdv1alpha1.MetricsExporterSpec{Enable: ptrBool(true)},
			NodeSelector:    map[string]string{"node.kubernetes.io/instance-type": "g4dn.xlarge"},
			CommonConfig:    amdv1alpha1.CommonConfigSpec{InitContainerImage: "busybox:latest"},
		},
	}
	r, c := newReconciler(t, cr)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "default"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	for _, name := range []string{"amd-device-plugin", "amd-device-metrics-exporter", "amd-node-labeller"} {
		var ds appsv1.DaemonSet
		if err := c.Get(context.Background(), client.ObjectKey{Namespace: "gpu-operator", Name: name}, &ds); err != nil {
			t.Errorf("expected DaemonSet %s, got: %v", name, err)
			continue
		}
		// NodeSelector should reflect the CR's override, not the default.
		if got := ds.Spec.Template.Spec.NodeSelector["node.kubernetes.io/instance-type"]; got != "g4dn.xlarge" {
			t.Errorf("%s: nodeSelector override not applied, got %v", name, ds.Spec.Template.Spec.NodeSelector)
		}
		// Owner ref points back at the DeviceConfig for cascade GC.
		if len(ds.OwnerReferences) != 1 || ds.OwnerReferences[0].Kind != "DeviceConfig" {
			t.Errorf("%s: missing or wrong owner ref, got %+v", name, ds.OwnerReferences)
		}
	}

	var dep appsv1.Deployment
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "gpu-operator", Name: "amd-status-updater"}, &dep); err != nil {
		t.Errorf("expected status-updater Deployment: %v", err)
	}

	var svc corev1.Service
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "gpu-operator", Name: "amd-device-metrics-exporter"}, &svc); err != nil {
		t.Errorf("expected metrics-exporter Service: %v", err)
	}
}

func TestReconcile_DisableDevicePluginDeletes(t *testing.T) {
	cr := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec:       amdv1alpha1.DeviceConfigSpec{DevicePlugin: amdv1alpha1.DevicePluginSpec{Enable: ptrBool(true)}},
	}
	r, c := newReconciler(t, cr)
	ctx := context.Background()

	// First reconcile: DS created.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: "default"}}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	var ds appsv1.DaemonSet
	if err := c.Get(ctx, client.ObjectKey{Namespace: "gpu-operator", Name: "amd-device-plugin"}, &ds); err != nil {
		t.Fatalf("DS missing after first reconcile: %v", err)
	}

	// Disable: re-Get to grab the latest resourceVersion (the first
	// Reconcile bumped it via status update), then Update.
	if err := c.Get(ctx, client.ObjectKey{Name: "default"}, cr); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	cr.Spec.DevicePlugin.Enable = ptrBool(false)
	if err := c.Update(ctx, cr); err != nil {
		t.Fatalf("update CR: %v", err)
	}

	// Second reconcile: DS deleted.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: "default"}}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	err := c.Get(ctx, client.ObjectKey{Namespace: "gpu-operator", Name: "amd-device-plugin"}, &ds)
	if err == nil {
		t.Error("device-plugin DS should be deleted after enable=false")
	} else if !apierrors.IsNotFound(err) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestReconcile_CRMissingIsNoop(t *testing.T) {
	r, _ := newReconciler(t)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Name: "nonexistent"}}); err != nil {
		t.Fatalf("expected no error for missing CR, got: %v", err)
	}
}

// TestReconcile_CPXMultipliesAdvertisedCapacity is #48's acceptance
// shape: switching computePartition.mode=cpx + count=N makes the
// device-plugin DS advertise gpusPerNode × N devices; switching back
// to spx restores. metrics-exporter DS gains partition flags so it can
// stamp partition_mode/partition_id labels.
func TestReconcile_CPXMultipliesAdvertisedCapacity(t *testing.T) {
	cr := &amdv1alpha1.DeviceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: amdv1alpha1.DeviceConfigSpec{
			ComputePartition: amdv1alpha1.ComputePartitionSpec{Mode: "cpx", Count: 4},
		},
	}
	r, c := newReconciler(t, cr)
	ctx := context.Background()

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: "default"}}); err != nil {
		t.Fatalf("reconcile cpx: %v", err)
	}

	// GPUsPerNode=2 (newReconciler default) × count=4 → device-plugin
	// should advertise 8.
	var devPlugin appsv1.DaemonSet
	if err := c.Get(ctx, client.ObjectKey{Namespace: "gpu-operator", Name: "amd-device-plugin"}, &devPlugin); err != nil {
		t.Fatalf("device-plugin DS missing: %v", err)
	}
	if got := devPlugin.Spec.Template.Spec.Containers[0].Args; !containsArg(got, "--gpus-per-node=8") {
		t.Errorf("device-plugin --gpus-per-node: got %v, want --gpus-per-node=8", got)
	}

	// metrics-exporter keeps --gpus-per-node=2 (physical count) and
	// adds --partition-mode=cpx + --partition-count=4 so it stamps
	// partition labels.
	var exporter appsv1.DaemonSet
	if err := c.Get(ctx, client.ObjectKey{Namespace: "gpu-operator", Name: "amd-device-metrics-exporter"}, &exporter); err != nil {
		t.Fatalf("metrics-exporter DS missing: %v", err)
	}
	args := exporter.Spec.Template.Spec.Containers[0].Args
	if !containsArg(args, "--gpus-per-node=2") ||
		!containsArg(args, "--partition-mode=cpx") ||
		!containsArg(args, "--partition-count=4") {
		t.Errorf("metrics-exporter args missing partition info: %v", args)
	}

	// Flip back to spx — device-plugin reverts to gpusPerNode=2.
	if err := c.Get(ctx, client.ObjectKey{Name: "default"}, cr); err != nil {
		t.Fatalf("re-Get CR: %v", err)
	}
	cr.Spec.ComputePartition = amdv1alpha1.ComputePartitionSpec{Mode: "spx", Count: 1}
	if err := c.Update(ctx, cr); err != nil {
		t.Fatalf("update CR to spx: %v", err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: "default"}}); err != nil {
		t.Fatalf("reconcile spx: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "gpu-operator", Name: "amd-device-plugin"}, &devPlugin); err != nil {
		t.Fatalf("device-plugin DS missing after spx revert: %v", err)
	}
	if got := devPlugin.Spec.Template.Spec.Containers[0].Args; !containsArg(got, "--gpus-per-node=2") {
		t.Errorf("after spx revert, device-plugin --gpus-per-node: got %v, want --gpus-per-node=2", got)
	}
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}

func ptrBool(b bool) *bool { return &b }
