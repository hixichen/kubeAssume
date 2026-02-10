package gcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with minimal fields",
			config: Config{
				Bucket: "my-bucket",
			},
			wantErr: false,
		},
		{
			name: "valid config with all fields",
			config: Config{
				Bucket:              "my-bucket",
				Project:             "my-project",
				Prefix:              "oidc",
				UseWorkloadIdentity: true,
				CacheControl:        "max-age=300",
				ContentType:         "application/json",
			},
			wantErr: false,
		},
		{
			name:    "missing bucket",
			config:  Config{},
			wantErr: true,
			errMsg:  "bucket name is required",
		},
		{
			name: "bucket name too short",
			config: Config{
				Bucket: "ab",
			},
			wantErr: true,
			errMsg:  "bucket name must be between 3 and 63 characters",
		},
		{
			name: "bucket name too long",
			config: Config{
				Bucket: "a" + string(make([]byte, 63)),
			},
			wantErr: true,
			errMsg:  "bucket name must be between 3 and 63 characters",
		},
		{
			name: "bucket name with uppercase",
			config: Config{
				Bucket: "MyBucket",
			},
			wantErr: true,
			errMsg:  "invalid bucket name",
		},
		{
			name: "bucket name starting with dot",
			config: Config{
				Bucket: ".my-bucket",
			},
			wantErr: true,
			errMsg:  "invalid bucket name",
		},
		{
			name: "bucket name containing 'goog'",
			config: Config{
				Bucket: "my-goog-bucket",
			},
			wantErr: true,
			errMsg:  "bucket name cannot contain 'goog'",
		},
		{
			name: "bucket name in IP format",
			config: Config{
				Bucket: "192.168.1.1",
			},
			wantErr: true,
			errMsg:  "bucket name cannot be in IP address format",
		},
		{
			name: "bucket name with special characters",
			config: Config{
				Bucket: "my_bucket!",
			},
			wantErr: true,
			errMsg:  "invalid bucket name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_GetPublicURL(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name: "standard URL",
			config: Config{
				Bucket: "my-bucket",
			},
			expected: "https://storage.googleapis.com/my-bucket",
		},
		{
			name: "bucket with hyphens",
			config: Config{
				Bucket: "my-test-bucket",
			},
			expected: "https://storage.googleapis.com/my-test-bucket",
		},
		{
			name: "bucket with dots",
			config: Config{
				Bucket: "my.bucket",
			},
			expected: "https://storage.googleapis.com/my.bucket",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetPublicURL()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetDiscoveryPath(t *testing.T) {
	// GetDiscoveryPath always returns the bare path; prefix is now encoded in GetPublicURL.
	tests := []struct {
		name     string
		prefix   string
		expected string
	}{
		{
			name:     "with prefix",
			prefix:   "oidc",
			expected: ".well-known/openid-configuration",
		},
		{
			name:     "without prefix",
			prefix:   "",
			expected: ".well-known/openid-configuration",
		},
		{
			name:     "with nested prefix",
			prefix:   "v1/oidc",
			expected: ".well-known/openid-configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Prefix: tt.prefix}
			result := config.GetDiscoveryPath()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetJWKSPath(t *testing.T) {
	// GetJWKSPath returns the bare path (no prefix) in single-cluster mode.
	// In multi-cluster mode it returns the cluster sub-path.
	tests := []struct {
		name                string
		prefix              string
		multiClusterEnabled bool
		clusterID           string
		expected            string
	}{
		{
			name:     "with prefix single-cluster",
			prefix:   "oidc",
			expected: "openid/v1/jwks",
		},
		{
			name:     "without prefix",
			prefix:   "",
			expected: "openid/v1/jwks",
		},
		{
			name:                "multi-cluster mode",
			prefix:              "prod",
			multiClusterEnabled: true,
			clusterID:           "prod-us-west-2",
			expected:            "clusters/prod-us-west-2/openid/v1/jwks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Prefix: tt.prefix, MultiClusterEnabled: tt.multiClusterEnabled, ClusterID: tt.clusterID}
			result := config.GetJWKSPath()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetFullDiscoveryURL(t *testing.T) {
	config := Config{
		Bucket: "my-bucket",
		Prefix: "oidc",
	}
	expected := "https://storage.googleapis.com/my-bucket/oidc/.well-known/openid-configuration"
	result := config.GetFullDiscoveryURL()
	assert.Equal(t, expected, result)
}

func TestConfig_GetFullJWKSURL(t *testing.T) {
	config := Config{
		Bucket: "my-bucket",
		Prefix: "oidc",
	}
	expected := "https://storage.googleapis.com/my-bucket/oidc/openid/v1/jwks"
	result := config.GetFullJWKSURL()
	assert.Equal(t, expected, result)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.Equal(t, "max-age=300", config.CacheControl)
	assert.Equal(t, "application/json", config.ContentType)
	assert.True(t, config.UseWorkloadIdentity)
	assert.Empty(t, config.Bucket)
	assert.Empty(t, config.Project)
	assert.Empty(t, config.Prefix)
}

func TestValidateBucketName(t *testing.T) {
	tests := []struct {
		name    string
		bucket  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid bucket name",
			bucket:  "my-bucket",
			wantErr: false,
		},
		{
			name:    "valid bucket with dots",
			bucket:  "my.bucket.name",
			wantErr: false,
		},
		{
			name:    "valid bucket with hyphens",
			bucket:  "my-test-bucket",
			wantErr: false,
		},
		{
			name:    "valid bucket with numbers",
			bucket:  "bucket123",
			wantErr: false,
		},
		{
			name:    "empty bucket",
			bucket:  "",
			wantErr: true,
			errMsg:  "bucket name must be between 3 and 63 characters",
		},
		{
			name:    "too short",
			bucket:  "ab",
			wantErr: true,
			errMsg:  "bucket name must be between 3 and 63 characters",
		},
		{
			name:    "too long",
			bucket:  string(make([]byte, 64)),
			wantErr: true,
			errMsg:  "bucket name must be between 3 and 63 characters",
		},
		{
			name:    "starts with dot",
			bucket:  ".bucket",
			wantErr: true,
			errMsg:  "bucket name must contain only",
		},
		{
			name:    "ends with dot",
			bucket:  "bucket.",
			wantErr: true,
			errMsg:  "bucket name must contain only",
		},
		{
			name:    "contains 'goog'",
			bucket:  "goog-bucket",
			wantErr: true,
			errMsg:  "bucket name cannot contain 'goog'",
		},
		{
			name:    "IP address format",
			bucket:  "192.168.1.1",
			wantErr: true,
			errMsg:  "bucket name cannot be in IP address format",
		},
		{
			name:    "uppercase letters",
			bucket:  "MyBucket",
			wantErr: true,
			errMsg:  "bucket name must contain only",
		},
		{
			name:    "special characters",
			bucket:  "bucket!name",
			wantErr: true,
			errMsg:  "bucket name must contain only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBucketName(tt.bucket)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
