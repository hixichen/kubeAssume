// Package azure provides Azure Blob Storage implementation of the Publisher interface
package azure

import (
	"fmt"
	"path"
)

// Config holds Azure Blob Storage configuration.
type Config struct {
	StorageAccount     string `mapstructure:"storageAccount"`
	Container          string `mapstructure:"container"`
	Prefix             string `mapstructure:"prefix,omitempty"`
	UseManagedIdentity bool   `mapstructure:"useManagedIdentity,omitempty"`
	TenantID           string `mapstructure:"tenantId,omitempty"`
	ClientID           string `mapstructure:"clientId,omitempty"`
	ClientSecret       string `mapstructure:"clientSecret,omitempty"`
	CacheControl       string `mapstructure:"cacheControl,omitempty"`
	ContentType        string `mapstructure:"contentType,omitempty"`

	// MultiClusterEnabled enables multi-cluster shared issuer mode
	MultiClusterEnabled bool
	// ClusterID is the unique identifier for this cluster within the group
	ClusterID string
}

// Validate validates the Azure configuration.
func (c Config) Validate() error {
	if c.StorageAccount == "" {
		return fmt.Errorf("storage account is required")
	}
	if c.Container == "" {
		return fmt.Errorf("container is required")
	}
	return nil
}

// GetDiscoveryPath returns the path for the discovery document.
func (c Config) GetDiscoveryPath() string {
	return path.Join(c.Prefix, ".well-known", "openid-configuration")
}

// GetJWKSPath returns the path for the JWKS
// In multi-cluster mode, writes to the cluster-specific sub-path.
func (c Config) GetJWKSPath() string {
	if c.MultiClusterEnabled {
		return path.Join(c.Prefix, "clusters", c.ClusterID, "openid", "v1", "jwks")
	}
	return path.Join(c.Prefix, "openid", "v1", "jwks")
}

// GetRootJWKSPath returns the root JWKS path (for aggregated writes in multi-cluster mode).
func (c Config) GetRootJWKSPath() string {
	return path.Join(c.Prefix, "openid", "v1", "jwks")
}

// GetClusterJWKSPath returns the cluster-specific JWKS path for the given clusterID.
func (c Config) GetClusterJWKSPath(clusterID string) string {
	return path.Join(c.Prefix, "clusters", clusterID, "openid", "v1", "jwks")
}

// GetPublicURL returns the public URL for the issuer.
func (c Config) GetPublicURL() string {
	return fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s", c.StorageAccount, c.Container, c.Prefix)
}
