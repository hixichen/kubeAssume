// Package s3 provides an S3 implementation of the Publisher interface
package s3

import (
	"fmt"
	"regexp"
	"strings"
)

// Config holds configuration for S3 publisher.
type Config struct {
	// Bucket is the S3 bucket name (required)
	Bucket string

	// Region is the AWS region (required)
	Region string

	// Prefix is an optional path prefix within the bucket
	Prefix string

	// UseIRSA indicates whether to use IRSA for credentials (recommended)
	UseIRSA bool

	// Endpoint is an optional custom S3 endpoint (for testing with minio)
	Endpoint string

	// ForcePathStyle forces path-style addressing (for minio compatibility)
	ForcePathStyle bool

	// CacheControl is the Cache-Control header value (default: "max-age=300")
	CacheControl string

	// ContentType is the Content-Type header value (default: "application/json")
	ContentType string

	// MultiClusterEnabled enables multi-cluster shared issuer mode
	MultiClusterEnabled bool

	// ClusterID is the unique identifier for this cluster within the group
	ClusterID string
}

// Validate validates the S3 configuration.
func (c *Config) Validate() error {
	// Check bucket is not empty
	if c.Bucket == "" {
		return fmt.Errorf("bucket name is required")
	}

	// Check region is not empty
	if c.Region == "" {
		return fmt.Errorf("region is required")
	}

	// Validate bucket name format
	if err := validateBucketName(c.Bucket); err != nil {
		return fmt.Errorf("invalid bucket name: %w", err)
	}

	return nil
}

// GetPublicURL constructs the public URL for the bucket, including prefix if set.
func (c *Config) GetPublicURL() string {
	var base string
	// Handle custom endpoint case
	if c.Endpoint != "" {
		base = fmt.Sprintf("%s/%s", c.Endpoint, c.Bucket)
	} else {
		// Format: https://BUCKET.s3.REGION.amazonaws.com
		base = fmt.Sprintf("https://%s.s3.%s.amazonaws.com", c.Bucket, c.Region)
	}
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
		CacheControl: "max-age=300",
		ContentType:  "application/json",
		UseIRSA:      true,
	}
}

// validateBucketName validates S3 bucket naming rules.
func validateBucketName(bucket string) error {
	if len(bucket) < 3 || len(bucket) > 63 {
		return fmt.Errorf("bucket name must be between 3 and 63 characters")
	}

	// Check for valid characters (lowercase, numbers, hyphens, dots)
	validBucketName := regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`)
	if !validBucketName.MatchString(bucket) {
		return fmt.Errorf("bucket name must contain only lowercase letters, numbers, hyphens, and dots, and must start and end with a letter or number")
	}

	// Check doesn't start/end with hyphen
	if strings.HasPrefix(bucket, "-") || strings.HasSuffix(bucket, "-") {
		return fmt.Errorf("bucket name cannot start or end with a hyphen")
	}

	// Check not IP address format
	ipPattern := regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	if ipPattern.MatchString(bucket) {
		return fmt.Errorf("bucket name cannot be in IP address format")
	}

	// Check for consecutive periods
	if strings.Contains(bucket, "..") {
		return fmt.Errorf("bucket name cannot contain consecutive periods")
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
