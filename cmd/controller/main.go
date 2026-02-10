// Package main provides the controller entrypoint for kubeassume
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/hixichen/kube-iam-assume/internal/controller"
	"github.com/hixichen/kube-iam-assume/pkg/bridge"
	"github.com/hixichen/kube-iam-assume/pkg/config"
	"github.com/hixichen/kube-iam-assume/pkg/constants"
	"github.com/hixichen/kube-iam-assume/pkg/publisher"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
	"github.com/hixichen/kube-iam-assume/pkg/rotation"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func main() {
	var metricsAddr, probeAddr string
	var configPath string

	// Parse flags
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&configPath, "config", "/etc/kubeassume/config.yaml", "Path to the configuration file.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrllog.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	logger := slog.Default()

	// Load config
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Get Kubernetes config
	k8sCfg := ctrl.GetConfigOrDie()

	// Create manager
	mgr, err := ctrl.NewManager(k8sCfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         cfg.Controller.LeaderElection.Enabled,
		LeaderElectionID:       cfg.Controller.LeaderElection.ID,
	})
	if err != nil {
		logger.Error("unable to create manager", "error", err)
		os.Exit(1)
	}

	// Initialize components
	if err := initializeComponents(mgr, cfg, logger); err != nil {
		logger.Error("failed to initialize components", "error", err)
		os.Exit(1)
	}

	// Add health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error("unable to set up health check", "error", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error("unable to set up ready check", "error", err)
		os.Exit(1)
	}

	logger.Info("starting manager",
		"syncPeriod", cfg.Controller.SyncPeriod,
		"publisherType", cfg.Publisher.Type,
	)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error("problem running manager", "error", err)
		os.Exit(1)
	}
}

// aggregationPoller is a leader-only runnable that periodically aggregates
// JWKS from all cluster sub-paths and publishes the merged result.
type aggregationPoller struct {
	aggregator          iface.MultiClusterAggregator
	aggregationInterval time.Duration
	clusterTTL          time.Duration
	logger              *slog.Logger
}

// NeedLeaderElection ensures only the elected leader runs aggregation.
func (a *aggregationPoller) NeedLeaderElection() bool { return true }

// Start begins the aggregation polling loop.
func (a *aggregationPoller) Start(ctx context.Context) error {
	a.logger.Info("Starting aggregation poller", "interval", a.aggregationInterval, "clusterTTL", a.clusterTTL)
	ticker := time.NewTicker(a.aggregationInterval)
	defer ticker.Stop()

	// Run immediately on leader election win
	a.aggregate(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.aggregate(ctx)
		}
	}
}

// aggregate fetches all cluster JWKS, prunes stale clusters, merges keys, and publishes.
func (a *aggregationPoller) aggregate(ctx context.Context) {
	clusterJWKS, err := a.aggregator.ListClusterJWKS(ctx)
	if err != nil {
		a.logger.Error("failed to list cluster JWKS", "error", err)
		return
	}

	lastModified, err := a.aggregator.GetClusterLastModified(ctx)
	if err != nil {
		a.logger.Error("failed to get cluster last-modified times", "error", err)
		return
	}

	// Prune clusters whose JWKS haven't been updated within the TTL
	now := time.Now()
	for clusterID, t := range lastModified {
		if now.Sub(t) > a.clusterTTL {
			a.logger.Info("pruning stale cluster from aggregation", "clusterID", clusterID, "lastModified", t)
			delete(clusterJWKS, clusterID)
		}
	}

	if len(clusterJWKS) == 0 {
		a.logger.Debug("no active cluster JWKS to aggregate")
		return
	}

	// Merge all keys, deduplicating by kid
	merged := mergeJWKS(clusterJWKS)

	if err := a.aggregator.PublishAggregatedJWKS(ctx, merged); err != nil {
		a.logger.Error("failed to publish aggregated JWKS", "error", err)
		return
	}

	a.logger.Info("aggregated JWKS published",
		"clusters", len(clusterJWKS),
		"total_keys", len(merged.Keys),
	)
}

// mergeJWKS merges JWKS from multiple clusters, deduplicating by KeyID.
func mergeJWKS(clusterJWKS map[string]*bridge.JWKS) *bridge.JWKS {
	seen := make(map[string]struct{})
	merged := &bridge.JWKS{}
	for _, jwks := range clusterJWKS {
		if jwks == nil {
			continue
		}
		for _, key := range jwks.Keys {
			if _, exists := seen[key.Kid]; !exists {
				seen[key.Kid] = struct{}{}
				merged.Keys = append(merged.Keys, key)
			}
		}
	}
	return merged
}

// oidcPoller is a runnable that periodically fetches OIDC metadata
// and stores it in a ConfigMap. It's designed to be run by the
// leader-elected instance of the controller.
type oidcPoller struct {
	bridge     bridge.OIDCBridge
	syncPeriod time.Duration
	logger     *slog.Logger
}

// Start begins the polling loop.
func (p *oidcPoller) Start(ctx context.Context) error {
	p.logger.Info("Starting OIDC poller")
	ticker := time.NewTicker(p.syncPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.logger.Debug("Polling for OIDC metadata")
			if _, err := p.bridge.Fetch(ctx); err != nil {
				p.logger.Error("failed to fetch OIDC metadata", "error", err)
			}
		}
	}
}

// NeedLeaderElection indicates that this runnable needs leader election.
func (p *oidcPoller) NeedLeaderElection() bool {
	return true
}

// initializeComponents initializes all controller components.
func initializeComponents(mgr manager.Manager, cfg *config.Config, logger *slog.Logger) error {
	ctx := ctrl.SetupSignalHandler()
	_ = ctx

	k8sClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create bridge
	bridgeClient, err := initializeBridge(mgr.GetConfig(), k8sClient, constants.DefaultNamespace, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize bridge: %w", err)
	}

	// Create and add the OIDC poller runnable
	syncPeriod, err := time.ParseDuration(cfg.Controller.SyncPeriod)
	if err != nil {
		return fmt.Errorf("invalid sync period: %w", err)
	}
	poller := &oidcPoller{
		bridge:     bridgeClient,
		syncPeriod: syncPeriod,
		logger:     logger.With("component", constants.ComponentNameOidcPoller),
	}
	if err := mgr.Add(poller); err != nil {
		return fmt.Errorf("failed to add OIDC poller to manager: %w", err)
	}

	// Create publisher
	pub, err := initializePublisher(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize publisher: %w", err)
	}

	// Validate publisher
	if err := pub.Validate(ctx); err != nil {
		logger.Warn("publisher validation failed (will retry)", "error", err)
	}

	// Create rotation manager
	overlapPeriod, err := time.ParseDuration(cfg.Controller.RotationOverlap)
	if err != nil {
		return fmt.Errorf("invalid rotation overlap period: %w", err)
	}
	rotMgr, err := initializeRotationManager(k8sClient, constants.DefaultNamespace, constants.DefaultRotationConfigMapName, overlapPeriod, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize rotation manager: %w", err)
	}

	// Create and register controller
	// TODO: Make namespace configurable
	ctrlCfg := controller.Config{
		SyncPeriod:          syncPeriod,
		Namespace:           constants.DefaultNamespace,
		PublicIssuerURL:     pub.GetPublicURL(), // Get public issuer URL from publisher
		MultiClusterEnabled: cfg.Controller.ClusterGroup != "",
	}

	rec := controller.NewOIDCBridgeReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(constants.EventRecorderName),
		bridgeClient,
		pub,
		rotMgr,
		ctrlCfg,
		logger,
	)

	if err := rec.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to set up controller: %w", err)
	}

	// Wire up aggregation poller in multi-cluster mode
	if cfg.Controller.ClusterGroup != "" {
		aggregator, ok := pub.(iface.MultiClusterAggregator)
		if !ok {
			return fmt.Errorf("publisher type %s does not implement MultiClusterAggregator (required when clusterGroup is set)", pub.Type())
		}

		aggregationInterval := 5 * time.Minute
		if cfg.Controller.AggregationInterval != "" {
			aggregationInterval, err = time.ParseDuration(cfg.Controller.AggregationInterval)
			if err != nil {
				return fmt.Errorf("invalid aggregationInterval: %w", err)
			}
		}

		clusterTTL := 48 * time.Hour
		if cfg.Controller.ClusterTTL != "" {
			clusterTTL, err = time.ParseDuration(cfg.Controller.ClusterTTL)
			if err != nil {
				return fmt.Errorf("invalid clusterTTL: %w", err)
			}
		}

		aggPoller := &aggregationPoller{
			aggregator:          aggregator,
			aggregationInterval: aggregationInterval,
			clusterTTL:          clusterTTL,
			logger:              logger.With("component", "aggregation-poller"),
		}
		if err := mgr.Add(aggPoller); err != nil {
			return fmt.Errorf("failed to add aggregation poller to manager: %w", err)
		}
		logger.Info("multi-cluster mode enabled",
			"clusterGroup", cfg.Controller.ClusterGroup,
			"clusterID", cfg.Controller.ClusterID,
			"aggregationInterval", aggregationInterval,
		)
	}

	return nil
}

// initializeBridge creates and initializes the OIDC bridge.
func initializeBridge(restConfig *rest.Config, k8sClient kubernetes.Interface, namespace string, logger *slog.Logger) (bridge.OIDCBridge, error) {
	// Create bridge config
	bridgeCfg := bridge.Config{
		RESTConfig: restConfig,
		K8sClient:  k8sClient,
		Namespace:  namespace,
		Logger:     logger,
	}

	// Create bridge
	bridgeClient, err := bridge.New(bridgeCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC bridge: %w", err)
	}

	return bridgeClient, nil
}

// initializePublisher creates and initializes the publisher.
func initializePublisher(cfg *config.Config, logger *slog.Logger) (iface.Publisher, error) {
	pubFactory := publisher.NewFactory(logger)

	// Initialize publisher (factory receives full config so it can wire clusterGroup as prefix)
	pub, err := pubFactory.Create(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create publisher: %w", err)
	}

	return pub, nil
}

// initializeRotationManager creates and initializes the rotation manager.
func initializeRotationManager(k8sClient kubernetes.Interface, namespace, configMapName string, overlapPeriod time.Duration, logger *slog.Logger) (rotation.Manager, error) {
	// Create ConfigMap store
	store := rotation.NewConfigMapStore(
		k8sClient,
		namespace,
		configMapName,
		logger,
	)

	// Create rotation config
	rotCfg := rotation.Config{
		OverlapPeriod: overlapPeriod,
		Namespace:     namespace,
		ConfigMapName: configMapName,
	}

	// Create rotation manager
	rotMgr := rotation.NewManager(store, rotCfg, logger)

	return rotMgr, nil
}
