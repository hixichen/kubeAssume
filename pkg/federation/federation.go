// Package federation provides interfaces for setting up OIDC identity federation
// with various cloud providers (AWS, GCP, Azure, OCI).
package federation

import "context"

// Provider defines the interface for cloud identity federation providers.
type Provider interface {
	// Setup creates the OIDC identity provider/federation
	Setup(ctx context.Context, cfg SetupConfig) (*SetupResult, error)

	// Validate checks if setup is valid
	Validate(ctx context.Context, issuerURL string) error

	// GetProviderInfo returns info about existing provider
	GetProviderInfo(ctx context.Context, issuerURL string) (*ProviderInfo, error)

	// Delete removes the OIDC provider
	Delete(ctx context.Context, issuerURL string) error

	// Type returns the provider type (aws, gcp, azure, oci)
	Type() string
}

// SetupConfig contains configuration for setting up OIDC federation.
type SetupConfig struct {
	IssuerURL string
	Audiences []string
	// Provider-specific options
	Options map[string]interface{}
}

// SetupResult contains the result of a federation setup.
type SetupResult struct {
	ProviderARN string // AWS: arn:aws:iam::..., GCP: projects/..., etc.
	Audiences   []string
	Thumbprint  string
}

// ProviderInfo contains information about an existing OIDC provider.
type ProviderInfo struct {
	ProviderARN   string
	IssuerURL     string
	Audiences     []string
	Thumbprint    string
	Status        string
	CreatedAt     string
	CloudProvider string
}

// ProviderType represents the type of cloud provider.
type ProviderType string

const (
	// ProviderTypeAWS is Amazon Web Services.
	ProviderTypeAWS ProviderType = "aws"
	// ProviderTypeGCP is Google Cloud Platform.
	ProviderTypeGCP ProviderType = "gcp"
	// ProviderTypeAzure is Microsoft Azure.
	ProviderTypeAzure ProviderType = "azure"
	// ProviderTypeOCI is Oracle Cloud Infrastructure.
	ProviderTypeOCI ProviderType = "oci"
)
