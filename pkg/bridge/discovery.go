package bridge

import (
	"fmt"
	"net/url"
	"strings"
)

// TransformDiscoveryDocument transforms the discovery document to use a public issuer URL
// This is necessary because the K8s API server issuer is internal,
// but we need to publish with a public URL (e.g., S3 bucket URL).
func TransformDiscoveryDocument(doc *DiscoveryDocument, publicIssuerURL string) (*DiscoveryDocument, error) {
	// Validate publicIssuerURL is a valid URL
	parsed, err := url.Parse(publicIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid public issuer URL: %w", err)
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("public issuer URL must use http or https scheme")
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("public issuer URL must have a host")
	}

	// Create new discovery document with public issuer
	transformed := &DiscoveryDocument{
		Issuer:                  publicIssuerURL,
		JWKSURI:                 "",
		AuthorizationEndpoint:   doc.AuthorizationEndpoint,
		ResponseTypesSupported:  doc.ResponseTypesSupported,
		SubjectTypesSupported:   doc.SubjectTypesSupported,
		IDTokenSigningAlgValues: doc.IDTokenSigningAlgValues,
		ClaimsSupported:         doc.ClaimsSupported,
	}

	// Update jwks_uri to point to public location
	jwksURI, err := buildPublicJWKSURI(publicIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to build public JWKS URI: %w", err)
	}
	transformed.JWKSURI = jwksURI

	return transformed, nil
}

// ValidateDiscoveryDocument validates a discovery document has required fields.
func ValidateDiscoveryDocument(doc *DiscoveryDocument) error {
	// Check issuer is not empty
	if doc.Issuer == "" {
		return fmt.Errorf("issuer is required")
	}

	// Check jwks_uri is not empty
	if doc.JWKSURI == "" {
		return fmt.Errorf("jwks_uri is required")
	}

	// Check required response types are present
	if len(doc.ResponseTypesSupported) == 0 {
		return fmt.Errorf("response_types_supported is required")
	}

	// Check subject types are present
	if len(doc.SubjectTypesSupported) == 0 {
		return fmt.Errorf("subject_types_supported is required")
	}

	// Check signing algorithms are present
	if len(doc.IDTokenSigningAlgValues) == 0 {
		return fmt.Errorf("id_token_signing_alg_values_supported is required")
	}

	return nil
}

// buildPublicJWKSURI constructs the public JWKS URI from the issuer URL.
func buildPublicJWKSURI(issuerURL string) (string, error) {
	parsed, err := url.Parse(issuerURL)
	if err != nil {
		return "", fmt.Errorf("invalid issuer URL: %w", err)
	}

	// Ensure path ends without trailing slash
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	parsed.Path = parsed.Path + "/openid/v1/jwks"

	return parsed.String(), nil
}
