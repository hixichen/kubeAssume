// Package oci provides OCI Object Storage implementation of the Publisher interface
package oci

import (
	"fmt"
	"path"
)

// Config holds OCI Object Storage configuration.
type Config struct {
	Bucket               string `mapstructure:"bucket"`
	Namespace            string `mapstructure:"namespace"`
	Region               string `mapstructure:"region,omitempty"`
	Prefix               string `mapstructure:"prefix,omitempty"`
	UseInstancePrincipal bool   `mapstructure:"useInstancePrincipal,omitempty"`
	UserID               string `mapstructure:"userId,omitempty"`
	Fingerprint          string `mapstructure:"fingerprint,omitempty"`
	KeyFile              string `mapstructure:"keyFile,omitempty"`
	TenancyID            string `mapstructure:"tenancyId,omitempty"`
	CacheControl         string `mapstructure:"cacheControl,omitempty"`
	ContentType          string `mapstructure:"contentType,omitempty"`

	// MultiClusterEnabled enables multi-cluster shared issuer mode
	MultiClusterEnabled bool
	// ClusterID is the unique identifier for this cluster within the group
	ClusterID string
}

// Validate validates the OCI configuration.
func (c Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if c.Namespace == "" {
		return fmt.Errorf("namespace is required")
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
	return fmt.Sprintf("https://objectstorage.%s.oraclecloud.com/n/%s/b/%s/o/%s", c.Region, c.Namespace, c.Bucket, c.Prefix)
}
