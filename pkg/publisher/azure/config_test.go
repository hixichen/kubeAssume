package azure

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
				StorageAccount: "myaccount",
				Container:      "mycontainer",
			},
			wantErr: false,
		},
		{
			name: "valid config with all fields",
			config: Config{
				StorageAccount:     "myaccount",
				Container:          "mycontainer",
				Prefix:             "oidc",
				UseManagedIdentity: true,
				TenantID:           "tenant-123",
				ClientID:           "client-123",
				ClientSecret:       "secret",
				CacheControl:       "max-age=300",
				ContentType:        "application/json",
			},
			wantErr: false,
		},
		{
			name: "missing storage account",
			config: Config{
				Container: "mycontainer",
			},
			wantErr: true,
			errMsg:  "storage account is required",
		},
		{
			name: "missing container",
			config: Config{
				StorageAccount: "myaccount",
			},
			wantErr: true,
			errMsg:  "container is required",
		},
		{
			name:    "empty config",
			config:  Config{},
			wantErr: true,
			errMsg:  "storage account is required",
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
			name: "standard URL without prefix",
			config: Config{
				StorageAccount: "myaccount",
				Container:      "mycontainer",
			},
			expected: "https://myaccount.blob.core.windows.net/mycontainer/",
		},
		{
			name: "URL with prefix",
			config: Config{
				StorageAccount: "myaccount",
				Container:      "mycontainer",
				Prefix:         "oidc",
			},
			expected: "https://myaccount.blob.core.windows.net/mycontainer/oidc",
		},
		{
			name: "URL with nested prefix",
			config: Config{
				StorageAccount: "myaccount",
				Container:      "mycontainer",
				Prefix:         "v1/oidc",
			},
			expected: "https://myaccount.blob.core.windows.net/mycontainer/v1/oidc",
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
