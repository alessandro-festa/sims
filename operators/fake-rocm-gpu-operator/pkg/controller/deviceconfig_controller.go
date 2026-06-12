// Package controller is the DeviceConfig reconciler. It watches
// DeviceConfig CRs (api/v1alpha1) and creates/updates the four child
// workloads sims's fake stack needs: device-plugin DS, metrics-exporter
// DS, status-updater Deployment, and node-labeller DS.
//
// The reconciler is configured at startup with all the per-workload
// settings (image, gpusPerNode, productName, etc.) via Config — the
// chart sets them via the controller subcommand's flags so this package
// stays free of any chart-template knowledge.
package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	amdv1alpha1 "github.com/alessandro-festa/sims/operators/fake-rocm-gpu-operator/api/v1alpha1"
)

// Config is the per-workload static configuration the chart passes to
// the controller binary. Held read-only by the Reconciler.
type Config struct {
	// Image is the fake-rocm-gpu-operator image reference (same one the
	// controller binary itself was built from). Used for all child
	// workloads — they each invoke a different subcommand on the same
	// image.
	Image           string
	ImagePullPolicy corev1.PullPolicy

	// Per-node fake GPU configuration. Propagated to device-plugin
	// (--gpus-per-node) and metrics-exporter (same).
	GPUsPerNode    int32
	ProductName    string
	GPUMemoryBytes int64

	// Resource name the device-plugin registers. amd.com/gpu by default.
	ResourceName string

	// Default nodeSelector for every child workload when the DeviceConfig
	// spec doesn't set one. Typically sims.io/gpu-vendor=amd.
	DefaultNodeSelector map[string]string

	// Default utilization range for pods without an annotation.
	DefaultUtilization string

	// Namespace where child workloads (and the DeviceConfig CR's RBAC)
	// live. The chart's release namespace.
	Namespace string
}

// DeviceConfigReconciler implements controller-runtime's Reconciler
// interface.
type DeviceConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Cfg    Config
}

// SetupWithManager wires the reconciler into the manager. We own
// DaemonSets and Deployments via owner refs, so Watches on those let
// the reconciler react to drift.
func (r *DeviceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&amdv1alpha1.DeviceConfig{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// Reconcile is the standard controller-runtime Reconciler entrypoint.
// On every DeviceConfig event (and any owned-resource event) we:
//  1. Compute desired child specs from the CR + Config.
//  2. Create-or-update each child via controllerutil.CreateOrUpdate.
//  3. Update the CR's status with what we observed.
func (r *DeviceConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("deviceconfig", req.Name)

	var cr amdv1alpha1.DeviceConfig
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil // CR deleted; owner refs GC the children.
		}
		return ctrl.Result{}, fmt.Errorf("get DeviceConfig: %w", err)
	}

	nodeSelector := cr.Spec.NodeSelector
	if len(nodeSelector) == 0 {
		nodeSelector = r.Cfg.DefaultNodeSelector
	}

	status := amdv1alpha1.DeviceConfigStatus{}

	// device-plugin DaemonSet
	if enabled(cr.Spec.DevicePlugin.Enable, true) {
		ds := r.buildDevicePluginDS(&cr, nodeSelector, cr.Spec.DevicePlugin.ImagePullPolicy)
		if err := r.applyDaemonSet(ctx, ds, &cr); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply device-plugin DS: %w", err)
		}
		status.DevicePluginReady = r.dsReady(ctx, ds.Namespace, ds.Name)
	} else {
		if err := r.deleteIfExists(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "amd-device-plugin", Namespace: r.Cfg.Namespace}}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// metrics-exporter DaemonSet
	if enabled(cr.Spec.MetricsExporter.Enable, true) {
		ds := r.buildMetricsExporterDS(&cr, nodeSelector)
		if err := r.applyDaemonSet(ctx, ds, &cr); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply metrics-exporter DS: %w", err)
		}
		if svc := r.buildMetricsExporterService(&cr); svc != nil {
			if err := r.applyService(ctx, svc, &cr); err != nil {
				return ctrl.Result{}, fmt.Errorf("apply metrics-exporter svc: %w", err)
			}
		}
		status.MetricsExporterReady = r.dsReady(ctx, ds.Namespace, ds.Name)
	} else {
		if err := r.deleteIfExists(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "amd-device-metrics-exporter", Namespace: r.Cfg.Namespace}}); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.deleteIfExists(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "amd-device-metrics-exporter", Namespace: r.Cfg.Namespace}}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// status-updater Deployment (always on for Phase 5+; not gated by spec)
	dep := r.buildStatusUpdaterDeployment(&cr, nodeSelector)
	if err := r.applyDeployment(ctx, dep, &cr); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply status-updater Deploy: %w", err)
	}

	// node-labeller DaemonSet (always on)
	labellerDS := r.buildNodeLabellerDS(&cr, nodeSelector)
	if err := r.applyDaemonSet(ctx, labellerDS, &cr); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply node-labeller DS: %w", err)
	}

	cr.Status = status
	if err := r.Status().Update(ctx, &cr); err != nil {
		// non-fatal: status update racing with another reconcile is fine
		logger.V(1).Info("status update failed (will retry)", "err", err)
	}

	return ctrl.Result{}, nil
}

// applyDaemonSet creates-or-updates a DS, setting cr as the owner so
// deletion cascades. controllerutil.CreateOrUpdate handles the
// get/diff/apply for us.
func (r *DeviceConfigReconciler) applyDaemonSet(ctx context.Context, want *appsv1.DaemonSet, owner *amdv1alpha1.DeviceConfig) error {
	desired := want.DeepCopy()
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, want, func() error {
		want.Spec = desired.Spec
		want.Labels = desired.Labels
		return controllerutil.SetControllerReference(owner, want, r.Scheme)
	})
	return err
}

func (r *DeviceConfigReconciler) applyDeployment(ctx context.Context, want *appsv1.Deployment, owner *amdv1alpha1.DeviceConfig) error {
	desired := want.DeepCopy()
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, want, func() error {
		want.Spec = desired.Spec
		want.Labels = desired.Labels
		return controllerutil.SetControllerReference(owner, want, r.Scheme)
	})
	return err
}

func (r *DeviceConfigReconciler) applyService(ctx context.Context, want *corev1.Service, owner *amdv1alpha1.DeviceConfig) error {
	desired := want.DeepCopy()
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, want, func() error {
		// Preserve ClusterIP across updates (immutable field).
		clusterIP := want.Spec.ClusterIP
		want.Spec = desired.Spec
		want.Spec.ClusterIP = clusterIP
		want.Labels = desired.Labels
		return controllerutil.SetControllerReference(owner, want, r.Scheme)
	})
	return err
}

func (r *DeviceConfigReconciler) deleteIfExists(ctx context.Context, obj client.Object) error {
	err := r.Delete(ctx, obj)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *DeviceConfigReconciler) dsReady(ctx context.Context, namespace, name string) bool {
	var ds appsv1.DaemonSet
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &ds); err != nil {
		return false
	}
	return ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
}

func enabled(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// --- workload builders ---------------------------------------------

func (r *DeviceConfigReconciler) commonLabels(app string) map[string]string {
	return map[string]string{
		"app":                          app,
		"app.kubernetes.io/name":       "fake-rocm-gpu-operator",
		"app.kubernetes.io/managed-by": "deviceconfig-controller",
		"app.kubernetes.io/part-of":    "sims",
	}
}

func (r *DeviceConfigReconciler) buildDevicePluginDS(cr *amdv1alpha1.DeviceConfig, nodeSelector map[string]string, pullPolicy string) *appsv1.DaemonSet {
	pp := corev1.PullPolicy(pullPolicy)
	if pp == "" {
		pp = r.Cfg.ImagePullPolicy
	}
	lbl := r.commonLabels("amd-device-plugin")
	priv := true
	root := int64(0)
	// Under CPX, the kubelet should see Count partitions per physical
	// GPU. effectiveGPUs is what --gpus-per-node is told.
	effectiveGPUs := r.Cfg.GPUsPerNode * partitionMultiplier(cr.Spec.ComputePartition)
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "amd-device-plugin", Namespace: r.Cfg.Namespace, Labels: lbl},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "amd-device-plugin"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: lbl},
				Spec: corev1.PodSpec{
					ServiceAccountName: "amd-device-plugin",
					NodeSelector:       nodeSelector,
					PriorityClassName:  "system-node-critical",
					Containers: []corev1.Container{{
						Name:            "device-plugin",
						Image:           r.Cfg.Image,
						ImagePullPolicy: pp,
						Args: []string{
							"device-plugin",
							fmt.Sprintf("--gpus-per-node=%d", effectiveGPUs),
							"--resource-name=" + r.Cfg.ResourceName,
							"--kubelet-socket-dir=/var/lib/kubelet/device-plugins",
							"--pod-resources-socket=/var/lib/kubelet/pod-resources/kubelet.sock",
						},
						Env: []corev1.EnvVar{{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}}},
						SecurityContext: &corev1.SecurityContext{
							Privileged: &priv,
							RunAsUser:  &root,
							RunAsGroup: &root,
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "kubelet-device-plugins", MountPath: "/var/lib/kubelet/device-plugins"},
							{Name: "pod-resources", MountPath: "/var/lib/kubelet/pod-resources", ReadOnly: true},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "kubelet-device-plugins", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet/device-plugins", Type: hostPathTypePtr(corev1.HostPathDirectory)}}},
						{Name: "pod-resources", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet/pod-resources", Type: hostPathTypePtr(corev1.HostPathDirectory)}}},
					},
				},
			},
		},
	}
}

func (r *DeviceConfigReconciler) buildMetricsExporterDS(cr *amdv1alpha1.DeviceConfig, nodeSelector map[string]string) *appsv1.DaemonSet {
	lbl := r.commonLabels("amd-device-metrics-exporter")
	nonRoot := int64(65532)
	tBool := true
	fBool := false
	// metrics-exporter sees the PHYSICAL gpu count + partition mode so
	// it can attach partition_mode/partition_id labels per series. The
	// device-plugin DS (above) advertises gpusPerNode × Count to the
	// kubelet under CPX, but the per-partition GPU identity belongs in
	// the exporter.
	mode, count := normalizePartition(cr.Spec.ComputePartition)
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "amd-device-metrics-exporter", Namespace: r.Cfg.Namespace, Labels: lbl},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "amd-device-metrics-exporter"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: lbl},
				Spec: corev1.PodSpec{
					ServiceAccountName: "amd-device-metrics-exporter",
					NodeSelector:       nodeSelector,
					Containers: []corev1.Container{{
						Name:            "metrics-exporter",
						Image:           r.Cfg.Image,
						ImagePullPolicy: r.Cfg.ImagePullPolicy,
						Args: []string{
							"metrics-exporter",
							"--listen=:5000",
							fmt.Sprintf("--gpus-per-node=%d", r.Cfg.GPUsPerNode),
							"--product-name=" + r.Cfg.ProductName,
							fmt.Sprintf("--memory-bytes=%d", r.Cfg.GPUMemoryBytes),
							"--topology-namespace=" + r.Cfg.Namespace,
							"--partition-mode=" + mode,
							fmt.Sprintf("--partition-count=%d", count),
							"--default-utilization=" + r.Cfg.DefaultUtilization,
						},
						Env:   []corev1.EnvVar{{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}}},
						Ports: []corev1.ContainerPort{{Name: "gpu-metrics", ContainerPort: 5000, Protocol: corev1.ProtocolTCP}},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:                &nonRoot,
							RunAsGroup:               &nonRoot,
							RunAsNonRoot:             &tBool,
							AllowPrivilegeEscalation: &fBool,
							ReadOnlyRootFilesystem:   &tBool,
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
}

func (r *DeviceConfigReconciler) buildMetricsExporterService(_ *amdv1alpha1.DeviceConfig) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "amd-device-metrics-exporter", Namespace: r.Cfg.Namespace, Labels: r.commonLabels("amd-device-metrics-exporter")},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "amd-device-metrics-exporter"},
			Ports:    []corev1.ServicePort{{Name: "gpu-metrics", Port: 5000, TargetPort: intstr.FromString("gpu-metrics"), Protocol: corev1.ProtocolTCP}},
		},
	}
}

func (r *DeviceConfigReconciler) buildStatusUpdaterDeployment(_ *amdv1alpha1.DeviceConfig, _ map[string]string) *appsv1.Deployment {
	lbl := r.commonLabels("amd-status-updater")
	replicas := int32(1)
	nonRoot := int64(65532)
	tBool := true
	fBool := false
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "amd-status-updater", Namespace: r.Cfg.Namespace, Labels: lbl},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "amd-status-updater"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: lbl},
				Spec: corev1.PodSpec{
					ServiceAccountName: "amd-status-updater",
					Containers: []corev1.Container{{
						Name:            "status-updater",
						Image:           r.Cfg.Image,
						ImagePullPolicy: r.Cfg.ImagePullPolicy,
						Args: []string{
							"status-updater",
							"--topology-namespace=" + r.Cfg.Namespace,
							"--reconcile-interval=5s",
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:                &nonRoot,
							RunAsGroup:               &nonRoot,
							RunAsNonRoot:             &tBool,
							AllowPrivilegeEscalation: &fBool,
							ReadOnlyRootFilesystem:   &tBool,
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
}

func (r *DeviceConfigReconciler) buildNodeLabellerDS(_ *amdv1alpha1.DeviceConfig, nodeSelector map[string]string) *appsv1.DaemonSet {
	lbl := r.commonLabels("amd-node-labeller")
	nonRoot := int64(65532)
	tBool := true
	fBool := false
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "amd-node-labeller", Namespace: r.Cfg.Namespace, Labels: lbl},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "amd-node-labeller"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: lbl},
				Spec: corev1.PodSpec{
					ServiceAccountName: "amd-node-labeller",
					NodeSelector:       nodeSelector,
					Containers: []corev1.Container{{
						Name:            "node-labeller",
						Image:           r.Cfg.Image,
						ImagePullPolicy: r.Cfg.ImagePullPolicy,
						Args: []string{
							"node-labeller",
							"--product-name=" + r.Cfg.ProductName,
						},
						Env: []corev1.EnvVar{{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}}},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:                &nonRoot,
							RunAsGroup:               &nonRoot,
							RunAsNonRoot:             &tBool,
							AllowPrivilegeEscalation: &fBool,
							ReadOnlyRootFilesystem:   &tBool,
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
}

func hostPathTypePtr(t corev1.HostPathType) *corev1.HostPathType { return &t }

// normalizePartition coerces the CR's ComputePartition into the
// invariant the workloads expect: under SPX (or empty), Count is 1.
// Returns (mode, count) ready to forward as flags.
func normalizePartition(p amdv1alpha1.ComputePartitionSpec) (string, int32) {
	if p.Mode != "cpx" {
		return "spx", 1
	}
	if p.Count < 1 {
		return "cpx", 1
	}
	return "cpx", p.Count
}

// partitionMultiplier returns Count under CPX, 1 otherwise — used to
// multiply gpusPerNode when computing what the device-plugin advertises.
func partitionMultiplier(p amdv1alpha1.ComputePartitionSpec) int32 {
	_, count := normalizePartition(p)
	return count
}
