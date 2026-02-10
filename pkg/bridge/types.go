// Package bridge provides functionality to fetch OIDC metadata from Kubernetes API server.
// The K8s API server exposes OIDC discovery endpoints that require authentication,
// unlike standard public OIDC endpoints.
package bridge

import (
	"encoding/json"
	"fmt"
	"time"
)

// DiscoveryDocument represents the OIDC discovery document
// fetched from /.well-known/openid-configuration.
type DiscoveryDocument struct {
	Issuer                  string   `json:"issuer"`
	JWKSURI                 string   `json:"jwks_uri"`
	AuthorizationEndpoint   string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint           string   `json:"token_endpoint,omitempty"`
	UserInfoEndpoint        string   `json:"userinfo_endpoint,omitempty"`
	ResponseTypesSupported  []string `json:"response_types_supported"`
	GrantTypesSupported     []string `json:"grant_types_supported,omitempty"`
	SubjectTypesSupported   []string `json:"subject_types_supported"`
	IDTokenSigningAlgValues []string `json:"id_token_signing_alg_values_supported"`
	ClaimsSupported         []string `json:"claims_supported,omitempty"`
	ScopesSupported         []string `json:"scopes_supported,omitempty"`
}

// ToJSON serializes the discovery document to indented JSON.
func (d *DiscoveryDocument) ToJSON() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

// Validate checks that the discovery document has the required fields.
func (d *DiscoveryDocument) Validate() error {
	if d.Issuer == "" {
		return fmt.Errorf("issuer is required")
	}
	if d.JWKSURI == "" {
		return fmt.Errorf("jwks_uri is required")
	}
	return nil
}

// JWKS represents a JSON Web Key Set containing the public keys
// used to verify service account tokens.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// ToJSON serializes the JWKS to indented JSON.
func (j *JWKS) ToJSON() ([]byte, error) {
	return json.MarshalIndent(j, "", "  ")
}

// Validate checks that the JWKS has at least one valid key.
func (j *JWKS) Validate() error {
	if j == nil || len(j.Keys) == 0 {
		return fmt.Errorf("JWKS contains no keys")
	}
	for i := range j.Keys {
		if err := j.Keys[i].Validate(); err != nil {
			return fmt.Errorf("key at index %d is invalid: %w", i, err)
		}
	}
	return nil
}

// JWK represents a single JSON Web Key.
type JWK struct {
	Kty string `json:"kty"`           // Key type (e.g., "RSA")
	Kid string `json:"kid"`           // Key ID
	Alg string `json:"alg,omitempty"` // Algorithm (e.g., "RS256")
	Use string `json:"use,omitempty"` // Key use (e.g., "sig")
	N   string `json:"n,omitempty"`   // RSA modulus (base64url)
	E   string `json:"e,omitempty"`   // RSA exponent (base64url)
}

// Validate checks that the JWK has the required fields and is a supported key type.
func (j *JWK) Validate() error {
	if j.Kty == "" {
		return fmt.Errorf("key type (kty) is required")
	}
	if j.Kty != "RSA" {
		return fmt.Errorf("unsupported key type: %s (only RSA is supported)", j.Kty)
	}
	return nil
}

// FetchResult contains the result of fetching OIDC metadata.
type FetchResult struct {
	Discovery    *DiscoveryDocument
	JWKS         *JWKS
	FetchedAt    time.Time
	SourceIssuer string // Original issuer from K8s API server
}
