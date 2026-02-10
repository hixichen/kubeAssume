package oci

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
				Bucket:    "my-bucket",
				Namespace: "my-namespace",
			},
			wantErr: false,
		},
		{
			name: "valid config with all fields",
			config: Config{
				Bucket:               "my-bucket",
				Namespace:            "my-namespace",
				Region:               "us-ashburn-1",
				Prefix:               "oidc",
				UseInstancePrincipal: true,
				UserID:               "user-123",
				Fingerprint:          "aa:bb:cc",
				KeyFile:              "/path/to/key.pem",
				TenancyID:            "tenancy-123",
				CacheControl:         "max-age=300",
				ContentType:          "application/json",
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			config: Config{
				Namespace: "my-namespace",
			},
			wantErr: true,
			errMsg:  "bucket is required",
		},
		{
			name: "missing namespace",
			config: Config{
				Bucket: "my-bucket",
			},
			wantErr: true,
			errMsg:  "namespace is required",
		},
		{
			name:    "empty config",
			config:  Config{},
			wantErr: true,
			errMsg:  "bucket is required",
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
			name: "standard URL with region",
			config: Config{
				Bucket:    "my-bucket",
				Namespace: "my-namespace",
				Region:    "us-ashburn-1",
				Prefix:    "",
			},
			expected: "https://objectstorage.us-ashburn-1.oraclecloud.com/n/my-namespace/b/my-bucket/o/",
		},
		{
			name: "URL with prefix",
			config: Config{
				Bucket:    "my-bucket",
				Namespace: "my-namespace",
				Region:    "eu-frankfurt-1",
				Prefix:    "oidc",
			},
			expected: "https://objectstorage.eu-frankfurt-1.oraclecloud.com/n/my-namespace/b/my-bucket/o/oidc",
		},
		{
			name: "URL with nested prefix",
			config: Config{
				Bucket:    "my-bucket",
				Namespace: "my-namespace",
				Region:    "ap-mumbai-1",
				Prefix:    "v1/oidc",
			},
			expected: "https://objectstorage.ap-mumbai-1.oraclecloud.com/n/my-namespace/b/my-bucket/o/v1/oidc",
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
	tests := []struct {
		name     string
		prefix   string
		expected string
	}{
		{
			name:     "with prefix",
			prefix:   "oidc",
			expected: "oidc/.well-known/openid-configuration",
		},
		{
			name:     "without prefix",
			prefix:   "",
			expected: ".well-known/openid-configuration",
		},
		{
			name:     "with nested prefix",
			prefix:   "v1/oidc",
			expected: "v1/oidc/.well-known/openid-configuration",
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
	tests := []struct {
		name     string
		prefix   string
		expected string
	}{
		{
			name:     "with prefix",
			prefix:   "oidc",
			expected: "oidc/openid/v1/jwks",
		},
		{
			name:     "without prefix",
			prefix:   "",
			expected: "openid/v1/jwks",
		},
		{
			name:     "with nested prefix",
			prefix:   "v1/oidc",
			expected: "v1/oidc/openid/v1/jwks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Prefix: tt.prefix}
			result := config.GetJWKSPath()
			assert.Equal(t, tt.expected, result)
		})
	}
}
