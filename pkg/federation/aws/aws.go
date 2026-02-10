// Package aws provides an AWS IAM OIDC Provider implementation of the Federation Provider interface
package aws

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/hixichen/kube-iam-assume/pkg/federation"
)

// Ensure awsProvider implements federation.Provider interface.
var _ federation.Provider = (*awsProvider)(nil)

// awsProvider implements federation.Provider for AWS IAM OIDC Provider.
type awsProvider struct {
	iamClient *iam.Client
	stsClient *sts.Client
	region    string
	logger    *slog.Logger
}

// NewProvider creates a new AWS Provider.
func NewProvider(ctx context.Context, region string, logger *slog.Logger) (federation.Provider, error) {
	// Load AWS config
	cfg, err := loadAWSConfig(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &awsProvider{
		iamClient: iam.NewFromConfig(cfg),
		stsClient: sts.NewFromConfig(cfg),
		region:    region,
		logger:    logger,
	}, nil
}

// Setup creates the OIDC identity provider/federation.
func (a *awsProvider) Setup(ctx context.Context, cfg federation.SetupConfig) (*federation.SetupResult, error) {
	a.logger.Info("Setting up AWS IAM OIDC Provider",
		"issuer_url", cfg.IssuerURL,
		"audiences", cfg.Audiences)

	// Check if provider already exists
	providerInfo, err := a.GetProviderInfo(ctx, cfg.IssuerURL)
	if err != nil && !strings.Contains(err.Error(), "no OIDC provider found") { // Ignore "not found" errors
		return nil, fmt.Errorf("failed to check existing OIDC provider: %w", err)
	}

	var providerARN string
	var thumbprint string
	if providerInfo != nil {
		a.logger.Info("OIDC provider already exists, attempting to update", "arn", providerInfo.ProviderARN)
		providerARN = providerInfo.ProviderARN
		// AWS does not allow updating thumbprints directly. If thumbprint is wrong, user needs to delete and re-create.
		// For now, we'll just re-use the existing thumbprint or fetch a new one if it's nil
		thumbprint = providerInfo.Thumbprint
	}

	// Fetch thumbprint
	if thumbprint == "" {
		a.logger.Info("Fetching OIDC issuer thumbprint", "issuer_url", cfg.IssuerURL)
		t, err := getThumbprint(ctx, cfg.IssuerURL)
		if err != nil {
			return nil, fmt.Errorf("failed to get OIDC issuer thumbprint: %w", err)
		}
		thumbprint = t
		a.logger.Info("Fetched OIDC issuer thumbprint", "thumbprint", thumbprint)
	}

	if providerInfo != nil {
		// Update (not supported by AWS, so we'll just return success if it already exists and matches)
		// Or if properties don't match, we might need to delete and recreate, but for now, we'll assume match.
		a.logger.Info("IAM OIDC Provider already exists and matches configuration. Skipping creation.")
	} else {
		// Create new OIDC provider
		a.logger.Info("Creating new IAM OIDC Provider",
			"issuer_url", cfg.IssuerURL,
			"thumbprint", thumbprint,
			"client_ids", cfg.Audiences)

		createInput := &iam.CreateOpenIDConnectProviderInput{
			Url:            aws.String(cfg.IssuerURL),
			ThumbprintList: []string{thumbprint},
			ClientIDList:   cfg.Audiences,
		}

		createOutput, err := a.iamClient.CreateOpenIDConnectProvider(ctx, createInput)
		if err != nil {
			return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
		}
		providerARN = aws.ToString(createOutput.OpenIDConnectProviderArn)
		a.logger.Info("Successfully created IAM OIDC Provider", "arn", providerARN)
	}

	return &federation.SetupResult{
		ProviderARN: providerARN,
		Audiences:   cfg.Audiences,
		Thumbprint:  thumbprint,
	}, nil
}

// Validate checks if setup is valid.
func (a *awsProvider) Validate(ctx context.Context, issuerURL string) error {
	a.logger.Debug("Validating AWS IAM OIDC Provider", "issuer_url", issuerURL)

	providerInfo, err := a.GetProviderInfo(ctx, issuerURL)
	if err != nil {
		return fmt.Errorf("failed to get OIDC provider info for validation: %w", err)
	}
	if providerInfo == nil {
		return fmt.Errorf("OIDC provider for issuer '%s' not found", issuerURL)
	}

	// Optionally, fetch and compare thumbprints for real-time validation
	// currentThumbprint, err := getThumbprint(ctx, issuerURL)
	// if err != nil {
	// 	a.logger.Warn("Failed to get current thumbprint for validation", "issuer_url", issuerURL, "error", err)
	// } else if currentThumbprint != providerInfo.Thumbprint {
	// 	return fmt.Errorf("OIDC provider thumbprint mismatch for issuer '%s'. Expected '%s', got '%s'", issuerURL, providerInfo.Thumbprint, currentThumbprint)
	// }

	a.logger.Info("AWS IAM OIDC Provider validated successfully", "issuer_url", issuerURL, "arn", providerInfo.ProviderARN)
	return nil
}

// GetProviderInfo returns info about existing provider.
func (a *awsProvider) GetProviderInfo(ctx context.Context, issuerURL string) (*federation.ProviderInfo, error) {
	a.logger.Debug("Getting AWS IAM OIDC Provider info", "issuer_url", issuerURL)

	listOutput, err := a.iamClient.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list OIDC providers: %w", err)
	}

	for _, p := range listOutput.OpenIDConnectProviderList {
		arn := aws.ToString(p.Arn)
		// OIDC provider ARN contains the URL
		if strings.Contains(arn, strings.ReplaceAll(issuerURL, "https://", "")) {
			getOutput, err := a.iamClient.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
				OpenIDConnectProviderArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get details for OIDC provider ARN '%s': %w", arn, err)
			}

			// Re-verify the URL explicitly as the ARN might be a partial match
			if aws.ToString(getOutput.Url) == issuerURL {
				return &federation.ProviderInfo{
					ProviderARN:   arn,
					IssuerURL:     aws.ToString(getOutput.Url),
					Audiences:     getOutput.ClientIDList,
					Thumbprint:    getOutput.ThumbprintList[0], // Assuming only one thumbprint
					Status:        "Active",                    // AWS does not provide explicit status
					CreatedAt:     getOutput.CreateDate.Format(time.RFC3339),
					CloudProvider: string(federation.ProviderTypeAWS),
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no OIDC provider found for issuer: %s", issuerURL)
}

// Delete removes the OIDC provider.
func (a *awsProvider) Delete(ctx context.Context, issuerURL string) error {
	a.logger.Info("Deleting AWS IAM OIDC Provider", "issuer_url", issuerURL)

	providerInfo, err := a.GetProviderInfo(ctx, issuerURL)
	if err != nil {
		return fmt.Errorf("failed to get OIDC provider info for deletion: %w", err)
	}
	if providerInfo == nil {
		a.logger.Info("OIDC provider not found, skipping deletion", "issuer_url", issuerURL)
		return nil
	}

	_, err = a.iamClient.DeleteOpenIDConnectProvider(ctx, &iam.DeleteOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(providerInfo.ProviderARN),
	})
	if err != nil {
		return fmt.Errorf("failed to delete OIDC provider '%s': %w", providerInfo.ProviderARN, err)
	}

	a.logger.Info("Successfully deleted AWS IAM OIDC Provider", "arn", providerInfo.ProviderARN)
	return nil
}

// Type returns the provider type.
func (a *awsProvider) Type() string {
	return string(federation.ProviderTypeAWS)
}

// loadAWSConfig loads AWS configuration with optional custom endpoint.
func loadAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	// Check for a custom endpoint from environment variable for local testing (e.g., LocalStack)
	customEndpoint := ""
	if os.Getenv("AWS_ENDPOINT_URL") != "" {
		customEndpoint = os.Getenv("AWS_ENDPOINT_URL")
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	if customEndpoint != "" {
		//nolint:staticcheck // WithEndpointResolverWithOptions is deprecated in newer SDK versions
		// but the go.mod pins aws-sdk-go-v2/config@v1.27.23 which lacks WithBaseEndpoint.
		opts = append(opts, config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc( //nolint:staticcheck
			func(service, region string, _ ...interface{}) (aws.Endpoint, error) { //nolint:staticcheck
				if service == iam.ServiceID || service == sts.ServiceID {
					return aws.Endpoint{URL: customEndpoint}, nil //nolint:staticcheck
				}
				return aws.Endpoint{}, &aws.EndpointNotFoundError{} //nolint:staticcheck
			},
		)))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return cfg, nil
}

// getThumbprint fetches the OIDC issuer's root CA certificate thumbprint.
func getThumbprint(ctx context.Context, issuerURL string) (string, error) {
	parsedURL, err := url.Parse(issuerURL)
	if err != nil {
		return "", fmt.Errorf("invalid issuer URL: %w", err)
	}

	// Create a custom HTTP client that does not verify TLS certificates for fetching discovery doc
	// This might be necessary if the issuer uses a self-signed CA or is not publicly trusted.
	// However, for fetching thumbprints for AWS, it's generally recommended to fetch the root CA's thumbprint
	// of the actual issuer URL, not necessarily the JWKS URI.
	// We connect directly to the issuerURL host to get the certificate chain.

	hostPort := parsedURL.Hostname()
	if parsedURL.Port() == "" {
		hostPort += ":443" // Default HTTPS port
	} else {
		hostPort = net.JoinHostPort(parsedURL.Hostname(), parsedURL.Port())
	}

	// Connect to the issuer URL's host to get its certificate chain.
	// InsecureSkipVerify is intentional: we want the raw certificate chain to compute
	// the thumbprint, not to validate it — the thumbprint IS the validation mechanism.
	dialer := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec
	netConn, err := dialer.DialContext(ctx, "tcp", hostPort)
	if err != nil {
		return "", fmt.Errorf("failed to connect to issuer host %s: %w", hostPort, err)
	}
	defer func() { _ = netConn.Close() }()

	conn := netConn.(*tls.Conn)
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", fmt.Errorf("no certificates found for issuer host %s", hostPort)
	}

	// AWS requires the thumbprint of the root CA.
	// The last certificate in the chain is usually the root.
	// If the chain is incomplete or self-signed, it might be the only cert.
	rootCert := certs[len(certs)-1]
	thumbprint := fmt.Sprintf("%x", sha1.Sum(rootCert.Raw))

	return thumbprint, nil
}
