package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hixichen/kube-iam-assume/pkg/constants"
)

// OIDCBridge defines the interface for fetching OIDC metadata from K8s API server.
type OIDCBridge interface {
	// FetchDiscoveryDocument retrieves the OIDC discovery document from
	// /.well-known/openid-configuration endpoint
	FetchDiscoveryDocument(ctx context.Context) (*DiscoveryDocument, error)

	// FetchJWKS retrieves the JSON Web Key Set from /openid/v1/jwks endpoint
	FetchJWKS(ctx context.Context) (*JWKS, error)

	// GetIssuer returns the original issuer URL from the K8s API server
	GetIssuer() string

	// Fetch retrieves both discovery document and JWKS in one call
	Fetch(ctx context.Context) (*FetchResult, error)
}

// Bridge implements OIDCBridge using client-go REST client.
type Bridge struct {
	restClient rest.Interface
	k8sClient  kubernetes.Interface
	namespace  string
	issuer     string
	logger     *slog.Logger
	config     Config
}

// Config holds configuration for creating a Bridge.
type Config struct {
	// RESTConfig is the Kubernetes REST client configuration (optional in tests)
	RESTConfig *rest.Config
	// K8sClient is the Kubernetes API client
	K8sClient kubernetes.Interface
	// Namespace is the namespace where the controller runs
	Namespace string
	// Logger is the structured logger to use
	Logger *slog.Logger

	// PublicIssuerURL is the public OIDC issuer URL (used for configuration validation)
	PublicIssuerURL string
	// SyncPeriod is the interval between OIDC metadata syncs
	SyncPeriod time.Duration
}

// Validate checks that the Config has the required fields for operation.
func (c Config) Validate() error {
	if c.PublicIssuerURL == "" {
		return fmt.Errorf("PublicIssuerURL is required")
	}
	if c.SyncPeriod <= 0 {
		return fmt.Errorf("SyncPeriod must be positive")
	}
	return nil
}

// New creates a new Bridge instance.
// The logger parameter, if non-nil, overrides Config.Logger.
// If RESTConfig is nil the REST client is not created (useful in unit tests).
func New(cfg Config, logger *slog.Logger) (*Bridge, error) {
	// Resolve logger
	if logger == nil {
		logger = cfg.Logger
	}
	if logger == nil {
		logger = slog.Default()
	}

	b := &Bridge{
		k8sClient: cfg.K8sClient,
		namespace: cfg.Namespace,
		issuer:    "",
		logger:    logger,
		config:    cfg,
	}

	// Only create the REST client when a config is provided
	if cfg.RESTConfig != nil {
		restClient, err := rest.RESTClientFor(cfg.RESTConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create REST client: %w", err)
		}
		b.restClient = restClient
	}

	return b, nil
}

// FetchDiscoveryDocument retrieves the OIDC discovery document.
func (b *Bridge) FetchDiscoveryDocument(ctx context.Context) (*DiscoveryDocument, error) {
	b.logger.Debug("Fetching discovery document")

	// Make GET request to /.well-known/openid-configuration
	result := b.restClient.Get().AbsPath("/.well-known/openid-configuration").Do(ctx)
	if result.Error() != nil {
		return nil, fmt.Errorf("failed to fetch discovery document: %w", result.Error())
	}

	// Read raw bytes
	data, err := result.Raw()
	if err != nil {
		return nil, fmt.Errorf("failed to read discovery document: %w", err)
	}

	// Parse JSON response into DiscoveryDocument
	doc, err := parseDiscoveryDocument(data)
	if err != nil {
		return nil, err
	}

	// Cache the issuer URL
	b.issuer = doc.Issuer

	b.logger.Debug("Successfully fetched discovery document",
		"issuer", doc.Issuer,
		"jwks_uri", doc.JWKSURI,
	)

	return doc, nil
}

// FetchJWKS retrieves the JSON Web Key Set.
func (b *Bridge) FetchJWKS(ctx context.Context) (*JWKS, error) {
	b.logger.Debug("Fetching JWKS")

	// Make GET request to /openid/v1/jwks
	result := b.restClient.Get().AbsPath("/openid/v1/jwks").Do(ctx)
	if result.Error() != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", result.Error())
	}

	// Read raw bytes
	data, err := result.Raw()
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS: %w", err)
	}

	// Parse JSON response into JWKS
	jwks, err := parseJWKS(data)
	if err != nil {
		return nil, err
	}

	// Validate keys are present
	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("JWKS contains no keys")
	}

	b.logger.Debug("Successfully fetched JWKS",
		"key_count", len(jwks.Keys),
	)

	return jwks, nil
}

// GetIssuer returns the original issuer URL.
func (b *Bridge) GetIssuer() string {
	return b.issuer
}

// Fetch retrieves both discovery document and JWKS.
func (b *Bridge) Fetch(ctx context.Context) (*FetchResult, error) {
	// Fetch discovery document
	discovery, err := b.FetchDiscoveryDocument(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch discovery document: %w", err)
	}

	// Fetch JWKS
	jwks, err := b.FetchJWKS(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	// Marshal discovery and JWKS to JSON
	discoveryJSON, err := json.MarshalIndent(discovery, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal discovery document to JSON: %w", err)
	}
	jwksJSON, err := json.MarshalIndent(jwks, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JWKS to JSON: %w", err)
	}

	// Store in ConfigMap
	cmClient := b.k8sClient.CoreV1().ConfigMaps(b.namespace)
	configMapName := constants.DefaultOIDCConfigMapName

	// Optimistic locking loop for ConfigMap update
	for {
		cm, err := cmClient.Get(ctx, configMapName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				// Create new ConfigMap
				newCm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configMapName,
						Namespace: b.namespace,
						Labels: map[string]string{
							"app.kubernetes.io/name":      constants.ControllerName,
							"app.kubernetes.io/component": "oidc-metadata",
						},
					},
					Data: map[string]string{
						"discovery.json": string(discoveryJSON),
						"jwks.json":      string(jwksJSON),
					},
				}
				_, err = cmClient.Create(ctx, newCm, metav1.CreateOptions{})
				if err != nil {
					return nil, fmt.Errorf("failed to create OIDC metadata ConfigMap: %w", err)
				}
				b.logger.Info("Created OIDC metadata ConfigMap", "name", configMapName, "namespace", b.namespace)
				break // Exit loop on successful create
			}
			return nil, fmt.Errorf("failed to get OIDC metadata ConfigMap: %w", err)
		}

		// Update existing ConfigMap
		cm.Data["discovery.json"] = string(discoveryJSON)
		cm.Data["jwks.json"] = string(jwksJSON)
		_, err = cmClient.Update(ctx, cm, metav1.UpdateOptions{})
		if err != nil {
			if errors.IsConflict(err) {
				b.logger.Debug("Conflict updating OIDC metadata ConfigMap, retrying...", "name", configMapName)
				continue // Retry on conflict
			}
			return nil, fmt.Errorf("failed to update OIDC metadata ConfigMap: %w", err)
		}
		b.logger.Debug("Updated OIDC metadata ConfigMap", "name", configMapName, "namespace", b.namespace)
		break // Exit loop on successful update
	}

	return &FetchResult{
		Discovery:    discovery,
		JWKS:         jwks,
		FetchedAt:    time.Now(),
		SourceIssuer: b.issuer,
	}, nil
}

// parseDiscoveryDocument parses JSON bytes into DiscoveryDocument.
func parseDiscoveryDocument(data []byte) (*DiscoveryDocument, error) {
	var doc DiscoveryDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse discovery document: %w", err)
	}
	return &doc, nil
}

// parseJWKS parses JSON bytes into JWKS.
func parseJWKS(data []byte) (*JWKS, error) {
	var jwks JWKS
	if err := json.Unmarshal(data, &jwks); err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}
	return &jwks, nil
}
