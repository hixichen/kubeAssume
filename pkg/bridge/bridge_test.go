package bridge

import (
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryDocument_ToJSON(t *testing.T) {
	dd := &DiscoveryDocument{
		Issuer:                  "https://example.com",
		JWKSURI:                 "https://example.com/openid/v1/jwks",
		AuthorizationEndpoint:   "https://example.com/auth",
		TokenEndpoint:           "https://example.com/token",
		UserInfoEndpoint:        "https://example.com/userinfo",
		ResponseTypesSupported:  []string{"code", "token"},
		GrantTypesSupported:     []string{"authorization_code", "refresh_token"},
		SubjectTypesSupported:   []string{"public"},
		IDTokenSigningAlgValues: []string{"RS256"},
		ClaimsSupported:         []string{"sub", "iss", "aud"},
		ScopesSupported:         []string{"openid"},
	}

	data, err := dd.ToJSON()
	require.NoError(t, err)

	// Verify JSON structure
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "https://example.com", result["issuer"])
	assert.Equal(t, "https://example.com/openid/v1/jwks", result["jwks_uri"])
}

func TestDiscoveryDocument_Validate(t *testing.T) {
	tests := []struct {
		name    string
		dd      *DiscoveryDocument
		wantErr bool
	}{
		{
			name: "valid document",
			dd: &DiscoveryDocument{
				Issuer:  "https://example.com",
				JWKSURI: "https://example.com/openid/v1/jwks",
			},
			wantErr: false,
		},
		{
			name: "missing issuer",
			dd: &DiscoveryDocument{
				JWKSURI: "https://example.com/openid/v1/jwks",
			},
			wantErr: true,
		},
		{
			name: "missing jwks_uri",
			dd: &DiscoveryDocument{
				Issuer: "https://example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dd.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestJWKS_ToJSON(t *testing.T) {
	jwk := JWK{
		Kty: "RSA",
		Kid: "test-key-1",
		N:   base64.RawURLEncoding.EncodeToString(big.NewInt(123456789).Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(65537).Bytes()),
		Alg: "RS256",
		Use: "sig",
	}

	jwks := &JWKS{
		Keys: []JWK{jwk},
	}

	data, err := jwks.ToJSON()
	require.NoError(t, err)

	// Verify JSON structure
	var result struct {
		Keys []JWK `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Len(t, result.Keys, 1)
	assert.Equal(t, "RSA", result.Keys[0].Kty)
	assert.Equal(t, "test-key-1", result.Keys[0].Kid)
}

func TestJWKS_Validate(t *testing.T) {
	tests := []struct {
		name    string
		jwks    *JWKS
		wantErr bool
	}{
		{
			name: "valid jwks",
			jwks: &JWKS{
				Keys: []JWK{
					{
						Kty: "RSA",
						Kid: "key-1",
						N:   base64.RawURLEncoding.EncodeToString(big.NewInt(123).Bytes()),
						E:   base64.RawURLEncoding.EncodeToString(big.NewInt(65537).Bytes()),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty keys",
			jwks: &JWKS{
				Keys: []JWK{},
			},
			wantErr: true,
		},
		{
			name: "missing kty",
			jwks: &JWKS{
				Keys: []JWK{
					{
						Kid: "key-1",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.jwks.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestJWK_Validate(t *testing.T) {
	tests := []struct {
		name    string
		jwk     JWK
		wantErr bool
	}{
		{
			name: "valid RSA key",
			jwk: JWK{
				Kty: "RSA",
				Kid: "test-key",
				N:   base64.RawURLEncoding.EncodeToString(big.NewInt(123).Bytes()),
				E:   base64.RawURLEncoding.EncodeToString(big.NewInt(65537).Bytes()),
			},
			wantErr: false,
		},
		{
			name: "missing kty",
			jwk: JWK{
				Kid: "test-key",
			},
			wantErr: true,
		},
		{
			name: "unsupported key type",
			jwk: JWK{
				Kty: "EC",
				Kid: "test-key",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.jwk.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOIDCBridge_New(t *testing.T) {
	br, err := New(Config{
		PublicIssuerURL: "https://example.com",
		SyncPeriod:      60 * time.Second,
	}, nil)

	require.NoError(t, err)
	assert.NotNil(t, br)
	assert.Equal(t, "https://example.com", br.config.PublicIssuerURL)
}

func TestOIDCBridge_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				PublicIssuerURL: "https://example.com",
				SyncPeriod:      60 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "missing issuer URL",
			config: Config{
				SyncPeriod: 60 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid sync period",
			config: Config{
				PublicIssuerURL: "https://example.com",
				SyncPeriod:      0,
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
