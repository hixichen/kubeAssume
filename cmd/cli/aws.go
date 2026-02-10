package main

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// AWSSetup handles AWS OIDC identity provider setup.
type AWSSetup struct {
	IssuerURL string
	Region    string
	Audiences []string
}

// Run executes the AWS setup.
func (s *AWSSetup) Run(ctx context.Context) (string, error) {
	// Validate inputs
	if err := s.Validate(); err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	// Verify issuer is reachable
	if err := s.FetchAndValidateIssuer(ctx); err != nil {
		return "", fmt.Errorf("issuer validation failed: %w", err)
	}

	// Get TLS thumbprint
	thumbprint, err := s.GetTLSThumbprint(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get TLS thumbprint: %w", err)
	}

	// Create OIDC provider
	arn, err := s.CreateOIDCProvider(ctx, thumbprint)
	if err != nil {
		return "", fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	return arn, nil
}

// Validate validates the setup configuration.
func (s *AWSSetup) Validate() error {
	// Validate issuer URL format
	if s.IssuerURL == "" {
		return fmt.Errorf("issuer URL is required")
	}

	parsed, err := url.Parse(s.IssuerURL)
	if err != nil {
		return fmt.Errorf("invalid issuer URL: %w", err)
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("issuer URL must use http or https scheme")
	}

	if parsed.Host == "" {
		return fmt.Errorf("issuer URL must have a host")
	}

	// Validate region
	if s.Region == "" {
		return fmt.Errorf("region is required")
	}

	// Validate audiences
	if len(s.Audiences) == 0 {
		return fmt.Errorf("at least one audience is required")
	}

	for _, audience := range s.Audiences {
		if audience == "" {
			return fmt.Errorf("audience cannot be empty")
		}
	}

	return nil
}

// FetchAndValidateIssuer fetches the OIDC discovery document to validate the issuer.
func (s *AWSSetup) FetchAndValidateIssuer(ctx context.Context) error {
	// Fetch /.well-known/openid-configuration
	discoveryURL := strings.TrimSuffix(s.IssuerURL, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch discovery document: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discovery endpoint returned status %d", resp.StatusCode)
	}

	// Validate issuer matches (would parse JSON and check issuer field in full implementation)
	return nil
}

// GetTLSThumbprint gets the SHA1 thumbprint of the TLS certificate.
func (s *AWSSetup) GetTLSThumbprint(ctx context.Context) (string, error) {
	// Parse issuer URL
	host, port, err := extractHost(s.IssuerURL)
	if err != nil {
		return "", err
	}

	// Connect with TLS
	thumbprint, err := getTLSCertThumbprint(ctx, host, port)
	if err != nil {
		return "", fmt.Errorf("failed to get TLS certificate: %w", err)
	}

	return thumbprint, nil
}

// CreateOIDCProvider creates the OIDC identity provider in AWS IAM.
func (s *AWSSetup) CreateOIDCProvider(ctx context.Context, thumbprint string) (string, error) {
	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(s.Region))
	if err != nil {
		return "", fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create IAM client
	client := iam.NewFromConfig(cfg)

	// Remove https:// prefix for provider URL
	providerURL := strings.TrimPrefix(s.IssuerURL, "https://")
	providerURL = strings.TrimPrefix(providerURL, "http://")

	input := &iam.CreateOpenIDConnectProviderInput{
		Url:            &providerURL,
		ClientIDList:   s.Audiences,
		ThumbprintList: []string{thumbprint},
	}

	output, err := client.CreateOpenIDConnectProvider(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	return *output.OpenIDConnectProviderArn, nil
}

// getTLSCertThumbprint gets the SHA1 thumbprint for a hostname.
func getTLSCertThumbprint(ctx context.Context, host string, port string) (string, error) {
	netConn, err := (&tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}}).DialContext(ctx, "tcp", host+":"+port)
	if err != nil {
		return "", fmt.Errorf("failed to connect: %w", err)
	}
	defer func() { _ = netConn.Close() }()

	conn := netConn.(*tls.Conn)
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", fmt.Errorf("no certificates found")
	}

	// Use the last certificate in the chain (root or intermediate)
	cert := certs[len(certs)-1]
	fingerprint := sha1.Sum(cert.Raw)
	return hex.EncodeToString(fingerprint[:]), nil
}

// extractHost extracts hostname and port from a URL.
func extractHost(issuerURL string) (host string, port string, err error) {
	parsed, err := url.Parse(issuerURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}

	host = parsed.Hostname()
	port = parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	return host, port, nil
}
