// Package gcs provides a GCS implementation of the Publisher interface
package gcs

import (
	"fmt"
	"regexp"
)

// Config holds configuration for GCS publisher.
type Config struct {
	// Bucket is the GCS bucket name (required)
	Bucket string

	// Project is the GCP project ID (optional, but recommended for explicit identity)
	Project string

	// Prefix is an optional path prefix within the bucket
	Prefix string

	// UseWorkloadIdentity indicates whether to use Workload Identity for credentials (recommended)
	UseWorkloadIdentity bool

	// CacheControl is the Cache-Control header value (default: "max-age=300")
	CacheControl string

	// ContentType is the Content-Type header value (default: "application/json")
	ContentType string

	// MultiClusterEnabled enables multi-cluster shared issuer mode
	MultiClusterEnabled bool

	// ClusterID is the unique identifier for this cluster within the group
	ClusterID string
}

// Validate validates the GCS configuration.
func (c *Config) Validate() error {
	// Check bucket is not empty
	if c.Bucket == "" {
		return fmt.Errorf("bucket name is required")
	}

	// Validate bucket name format
	if err := validateBucketName(c.Bucket); err != nil {
		return fmt.Errorf("invalid bucket name: %w", err)
	}

	return nil
}

// GetPublicURL constructs the public URL for the bucket, including prefix if set.
func (c *Config) GetPublicURL() string {
	base := fmt.Sprintf("https://storage.googleapis.com/%s", c.Bucket)
	if c.Prefix != "" {
		return base + "/" + c.Prefix
	}
	return base
}

// GetDiscoveryPath returns the path for the discovery document (no prefix — prefix is in GetPublicURL).
func (c *Config) GetDiscoveryPath() string {
	return ".well-known/openid-configuration"
}

// GetJWKSPath returns the path for the JWKS
// In multi-cluster mode, writes to the cluster-specific sub-path.
func (c *Config) GetJWKSPath() string {
	if c.MultiClusterEnabled {
		return "clusters/" + c.ClusterID + "/openid/v1/jwks"
	}
	return "openid/v1/jwks"
}

// GetRootJWKSPath returns the root JWKS path (for aggregated writes in multi-cluster mode).
func (c *Config) GetRootJWKSPath() string {
	return "openid/v1/jwks"
}

// GetClusterJWKSPath returns the cluster-specific JWKS path for the given clusterID.
func (c *Config) GetClusterJWKSPath(clusterID string) string {
	return "clusters/" + clusterID + "/openid/v1/jwks"
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		CacheControl:        "max-age=300",
		ContentType:         "application/json",
		UseWorkloadIdentity: true,
	}
}

// validateBucketName validates GCS bucket naming rules
// GCS bucket names must contain only lowercase letters, numeric characters, dashes (-), underscores (_), and dots (.).
// Names must start and end with a number or letter.
// Names must be between 3 and 63 characters long.
// Names containing dots require verification.
// Names cannot contain "goog" or look like IP addresses.
func validateBucketName(bucket string) error {
	if len(bucket) < 3 || len(bucket) > 63 {
		return fmt.Errorf("bucket name must be between 3 and 63 characters")
	}

	// Check for valid characters
	validBucketName := regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
	if !validBucketName.MatchString(bucket) {
		return fmt.Errorf("bucket name must contain only lowercase letters, numbers, dashes, underscores, and dots, and must start and end with a letter or number")
	}

	// Cannot contain "goog"
	if regexp.MustCompile(`goog`).MatchString(bucket) {
		return fmt.Errorf("bucket name cannot contain 'goog'")
	}

	// Cannot be in IP address format
	ipPattern := regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	if ipPattern.MatchString(bucket) {
		return fmt.Errorf("bucket name cannot be in IP address format")
	}

	return nil
}

// GetFullDiscoveryURL returns the complete public URL for the discovery document.
func (c *Config) GetFullDiscoveryURL() string {
	return fmt.Sprintf("%s/%s", c.GetPublicURL(), c.GetDiscoveryPath())
}

// GetFullJWKSURL returns the complete public URL for the JWKS.
func (c *Config) GetFullJWKSURL() string {
	return fmt.Sprintf("%s/%s", c.GetPublicURL(), c.GetJWKSPath())
}
