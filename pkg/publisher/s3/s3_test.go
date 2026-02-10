package s3

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Bucket: "my-bucket",
				Region: "us-west-2",
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			config: Config{
				Region: "us-west-2",
			},
			wantErr: true,
		},
		{
			name: "missing region",
			config: Config{
				Bucket: "my-bucket",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
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
				Region: "us-west-2",
			},
			expected: "https://my-bucket.s3.us-west-2.amazonaws.com",
		},
		{
			name: "with prefix",
			config: Config{
				Bucket: "my-bucket",
				Region: "us-west-2",
				Prefix: "oidc",
			},
			expected: "https://my-bucket.s3.us-west-2.amazonaws.com/oidc",
		},
		{
			name: "with endpoint override",
			config: Config{
				Bucket:   "my-bucket",
				Region:   "us-west-2",
				Endpoint: "https://minio.example.com",
			},
			expected: "https://minio.example.com/my-bucket",
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
	// GetDiscoveryPath always returns the bare path; prefix is now in GetPublicURL.
	config := Config{
		Prefix: "oidc",
	}
	assert.Equal(t, ".well-known/openid-configuration", config.GetDiscoveryPath())

	config2 := Config{}
	assert.Equal(t, ".well-known/openid-configuration", config2.GetDiscoveryPath())
}

func TestConfig_GetJWKSPath(t *testing.T) {
	// GetJWKSPath returns bare path in single-cluster mode; cluster sub-path in multi-cluster mode.
	config := Config{
		Prefix: "oidc",
	}
	assert.Equal(t, "openid/v1/jwks", config.GetJWKSPath())

	config2 := Config{}
	assert.Equal(t, "openid/v1/jwks", config2.GetJWKSPath())

	configMC := Config{
		MultiClusterEnabled: true,
		ClusterID:           "prod-us-west-2",
	}
	assert.Equal(t, "clusters/prod-us-west-2/openid/v1/jwks", configMC.GetJWKSPath())
}

func TestNew(t *testing.T) {
	// This test validates config validation only since we can't connect to real S3
	ctx := context.Background()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Bucket: "my-bucket",
				Region: "us-west-2",
			},
			wantErr: false,
		},
		{
			name: "invalid config - missing bucket",
			config: Config{
				Region: "us-west-2",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub, err := New(ctx, tt.config, nil)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, pub)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, pub)
			}
		})
	}
}

func TestPublisher_Type(t *testing.T) {
	pub := &Publisher{
		config: Config{
			Bucket: "test-bucket",
			Region: "us-west-2",
		},
	}
	assert.Equal(t, iface.PublisherType("s3"), pub.Type())
}

func TestPublisher_GetPublicURL(t *testing.T) {
	pub := &Publisher{
		config: Config{
			Bucket: "test-bucket",
			Region: "us-west-2",
		},
	}
	assert.Equal(t, "https://test-bucket.s3.us-west-2.amazonaws.com", pub.GetPublicURL())
}

func TestMarshalJSON(t *testing.T) {
	dd := &bridge.DiscoveryDocument{
		Issuer:  "https://example.com",
		JWKSURI: "https://example.com/jwks",
	}

	data, err := marshalJSON(dd)
	require.NoError(t, err)

	// Verify it's valid JSON and properly formatted
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "https://example.com", result["issuer"])
}
