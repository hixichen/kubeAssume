// Package controller provides the main reconciliation loop for the OIDC bridge
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
	"github.com/hixichen/kube-iam-assume/pkg/constants"
	"github.com/hixichen/kube-iam-assume/pkg/health"
	"github.com/hixichen/kube-iam-assume/pkg/metrics"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
	"github.com/hixichen/kube-iam-assume/pkg/rotation"
)

const (
	// DefaultSyncPeriod is the default interval between syncs.
	DefaultSyncPeriod = 60 * time.Second
	// EventReasonSynced is the event reason for successful sync.
	EventReasonSynced = "Synced"
	// EventReasonSyncFailed is the event reason for failed sync.
	EventReasonSyncFailed = "SyncFailed"
	// EventReasonKeyRotation is the event reason for key rotation.
	EventReasonKeyRotation = "KeyRotation"
)

// Config holds configuration for the controller.
type Config struct {
	// SyncPeriod is the interval between syncs
	SyncPeriod time.Duration
	// Namespace is the namespace where the controller runs
	Namespace string
	// PublicIssuerURL is the public URL for OIDC discovery
	PublicIssuerURL string
	// MultiClusterEnabled indicates that multi-cluster shared issuer mode is active
	MultiClusterEnabled bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		SyncPeriod: DefaultSyncPeriod,
		Namespace:  "kubeassume-system",
	}
}

// OIDCBridgeReconciler reconciles OIDC metadata from K8s to cloud storage.
type OIDCBridgeReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Components
	Bridge          bridge.OIDCBridge
	Publisher       iface.Publisher
	RotationManager rotation.Manager
	Health          *health.Health
	Metrics         *metrics.Metrics

	// Configuration
	Config Config
	Logger *slog.Logger

	// Internal state
	kubeClient kubernetes.Interface
	lastSync   time.Time
}

// NewOIDCBridgeReconciler creates a new reconciler.
func NewOIDCBridgeReconciler(
	c client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	bridgeClient bridge.OIDCBridge,
	pub iface.Publisher,
	rotMgr rotation.Manager,
	cfg Config,
	logger *slog.Logger,
) *OIDCBridgeReconciler {
	return &OIDCBridgeReconciler{
		Client:          c,
		Scheme:          scheme,
		Recorder:        recorder,
		Bridge:          bridgeClient,
		Publisher:       pub,
		RotationManager: rotMgr,
		Config:          cfg,
		Logger:          logger,
		Health:          health.New(logger),
		Metrics:         metrics.New(),
	}
}

// Reconcile is the main logic that is triggered by changes to the OIDC metadata ConfigMap.
func (r *OIDCBridgeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Debug("Reconciliation triggered by OIDC metadata ConfigMap change")

	// Get the OIDC metadata ConfigMap.
	var cm corev1.ConfigMap
	if err := r.Get(ctx, req.NamespacedName, &cm); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("OIDC metadata ConfigMap not found, skipping reconciliation", "name", req.Name, "namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get OIDC metadata ConfigMap: %w", err)
	}

	// Unmarshal discovery document
	discoveryData, ok := cm.Data["discovery.json"]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("discovery.json not found in OIDC metadata ConfigMap")
	}
	var discovery bridge.DiscoveryDocument
	if err := json.Unmarshal([]byte(discoveryData), &discovery); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to unmarshal discovery.json from ConfigMap: %w", err)
	}

	// Unmarshal JWKS
	jwksData, ok := cm.Data["jwks.json"]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("jwks.json not found in OIDC metadata ConfigMap")
	}
	var jwks bridge.JWKS
	if err := json.Unmarshal([]byte(jwksData), &jwks); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to unmarshal jwks.json from ConfigMap: %w", err)
	}

	// 1. Process rotation (detect key changes, merge overlap keys)
	mergedJWKS, events, err := r.processRotation(ctx, &jwks)
	if err != nil {
		r.Logger.Error("failed to process rotation", "error", err)
		return ctrl.Result{Requeue: true}, nil
	}

	// 2. Emit K8s events for each rotation event
	for _, event := range events {
		r.emitRotationEvent(event)
	}

	// 3. Transform discovery document and publish
	if err := r.publish(ctx, &discovery, mergedJWKS); err != nil {
		r.Logger.Error("failed to publish OIDC metadata", "error", err)
		r.Metrics.RecordPublishError(string(r.Publisher.Type()))
		return ctrl.Result{Requeue: true}, nil
	}

	// 4. Update active keys metric
	r.Metrics.SetActiveKeys(len(mergedJWKS.Keys))

	r.Logger.Info("Sync completed successfully",
		"key_count", len(mergedJWKS.Keys),
		"rotation_events", len(events),
	)

	r.Metrics.RecordSync("success")
	r.lastSync = time.Now()
	if pod, podErr := r.getControllerPod(ctx); podErr == nil && pod != nil {
		r.Recorder.Eventf(pod, corev1.EventTypeNormal, EventReasonSynced, "OIDC metadata synced successfully")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OIDCBridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Register health checks
	r.registerHealthChecks()

	// Set up kubernetes clientset for event emission
	k8sCfg := mgr.GetConfig()
	clientset, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}
	r.kubeClient = clientset

	// Watch for changes to the OIDC metadata ConfigMap
	return ctrl.NewControllerManagedBy(mgr).
		Named("oidcbridge").
		For(&corev1.ConfigMap{}).
		WithEventFilter(r.oidcMetadataConfigMapFilter()).
		Complete(r)
}

func (r *OIDCBridgeReconciler) oidcMetadataConfigMapFilter() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return r.isOIDCConfigMap(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.isOIDCConfigMap(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return r.isOIDCConfigMap(e.Object)
		},
	}
}

func (r *OIDCBridgeReconciler) isOIDCConfigMap(obj client.Object) bool {
	return obj.GetName() == constants.DefaultOIDCConfigMapName && obj.GetNamespace() == r.Config.Namespace
}

// processRotation handles key rotation detection and merging.
func (r *OIDCBridgeReconciler) processRotation(ctx context.Context, jwks *bridge.JWKS) (*bridge.JWKS, []rotation.Event, error) {
	// Process JWKS through rotation manager
	merged, events, err := r.RotationManager.ProcessJWKS(ctx, jwks)
	if err != nil {
		return nil, nil, fmt.Errorf("rotation processing failed: %w", err)
	}

	// Record rotation metrics
	for _, event := range events {
		switch event.Type {
		case rotation.EventNewKey:
			r.Metrics.RecordRotation("new_key")
		case rotation.EventKeyExpired:
			r.Metrics.RecordRotation("key_expired")
		}
	}

	return merged, events, nil
}

// publish publishes the OIDC metadata to the configured backend.
func (r *OIDCBridgeReconciler) publish(ctx context.Context, discovery *bridge.DiscoveryDocument, jwks *bridge.JWKS) error {
	publishStart := time.Now()

	// Transform discovery document with public issuer URL
	transformed, err := bridge.TransformDiscoveryDocument(discovery, r.Config.PublicIssuerURL)
	if err != nil {
		return fmt.Errorf("failed to transform discovery document: %w", err)
	}

	// Publish to configured backend
	if err := r.Publisher.Publish(ctx, transformed, jwks); err != nil {
		r.Metrics.RecordPublishError(string(r.Publisher.Type()))
		return fmt.Errorf("failed to publish: %w", err)
	}

	// Record publish duration and timestamp
	publishDuration := time.Since(publishStart).Seconds()
	r.Metrics.RecordSyncDuration("publish", publishDuration)
	r.Metrics.RecordPublish(float64(time.Now().Unix()))

	r.Logger.Debug("Published OIDC metadata",
		"publisher", r.Publisher.Type(),
		"public_url", r.Config.PublicIssuerURL,
		"duration_s", publishDuration,
	)

	return nil
}

// emitRotationEvent emits a Kubernetes event for key rotation.
func (r *OIDCBridgeReconciler) emitRotationEvent(event rotation.Event) {
	pod, err := r.getControllerPod(context.Background())
	if err != nil || pod == nil {
		r.Logger.Debug("Cannot emit K8s event, controller pod not found", "error", err)
		return
	}

	switch event.Type {
	case rotation.EventNewKey:
		r.Recorder.Eventf(pod, corev1.EventTypeNormal, EventReasonKeyRotation,
			"New signing key detected: %s", event.KeyID)
	case rotation.EventKeyExpired:
		r.Recorder.Eventf(pod, corev1.EventTypeNormal, EventReasonKeyRotation,
			"Signing key expired and removed: %s", event.KeyID)
	}
}

// registerHealthChecks registers health checks with the health manager.
func (r *OIDCBridgeReconciler) registerHealthChecks() {
	r.Health.Register("bridge", func(ctx context.Context) error {
		// Verify we can reach the API server OIDC endpoints
		_, err := r.Bridge.FetchDiscoveryDocument(ctx)
		return err
	})

	r.Health.Register("publisher", func(ctx context.Context) error {
		return r.Publisher.HealthCheck(ctx)
	})
}

// getControllerPod retrieves the controller pod for event emission.
// Uses the POD_NAME and POD_NAMESPACE env vars set via Downward API.
func (r *OIDCBridgeReconciler) getControllerPod(ctx context.Context) (*corev1.Pod, error) {
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	if podNamespace == "" {
		podNamespace = r.Config.Namespace
	}

	if podName == "" {
		return nil, nil
	}

	pod := &corev1.Pod{}
	key := types.NamespacedName{Name: podName, Namespace: podNamespace}
	if err := r.Get(ctx, key, pod); err != nil {
		return nil, fmt.Errorf("failed to get controller pod: %w", err)
	}

	return pod, nil
}

// InitialRequest returns a reconcile request to kick off the first sync.
// Call this when setting up the controller to enqueue the initial run.
func InitialRequest(namespace string) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "kubeassume-sync",
			Namespace: namespace,
		},
	}
}

// SetKubeClient sets the kubernetes clientset (for use in tests).
func (r *OIDCBridgeReconciler) SetKubeClient(clientset kubernetes.Interface) {
	r.kubeClient = clientset
}

// GetLastSync returns the time of the last successful sync.
func (r *OIDCBridgeReconciler) GetLastSync() time.Time {
	return r.lastSync
}
