package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/hixichen/kube-iam-assume/pkg/federation"
	awsfederation "github.com/hixichen/kube-iam-assume/pkg/federation/aws"
)

// newAWSCommand creates the AWS setup subcommand.
func newAWSCommand() *cobra.Command {
	var (
		issuerURL string
		region    string
		audience  []string
	)

	cmd := &cobra.Command{
		Use:   "aws",
		Short: "Setup AWS IAM OIDC Provider",
		Long: `Setup AWS IAM OIDC Provider for OIDC identity federation.

This command creates an OIDC identity provider in AWS IAM that trusts your
Kubernetes cluster's OIDC issuer, enabling pods to assume IAM roles
using service account tokens.`,
		Example: `  # Basic setup
  kubeassume setup aws --issuer-url https://my-bucket.s3.us-west-2.amazonaws.com

  # Setup with specific region and audience
  kubeassume setup aws \
    --issuer-url https://my-bucket.s3.us-west-2.amazonaws.com \
    --region us-west-2 \
    --audience sts.amazonaws.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAWSSetup(cmd.Context(), issuerURL, region, audience)
		},
	}

	cmd.Flags().StringVar(&issuerURL, "issuer-url", "", "OIDC issuer URL (required)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (required, or use AWS_REGION env var)")
	cmd.Flags().StringArrayVar(&audience, "audience", []string{"sts.amazonaws.com"}, "OIDC audience(s)")

	if err := cmd.MarkFlagRequired("issuer-url"); err != nil {
		panic(err)
	}

	return cmd
}

func runAWSSetup(ctx context.Context, issuerURL, region string, audiences []string) error {
	// Get region from environment if not provided
	if region == "" {
		region = os.Getenv("AWS_REGION")
		if region == "" {
			region = os.Getenv("AWS_DEFAULT_REGION")
		}
		if region == "" {
			return fmt.Errorf("region is required (set --region flag or AWS_REGION/AWS_DEFAULT_REGION env var)")
		}
	}

	fmt.Printf("Setting up AWS IAM OIDC Provider...\n")
	fmt.Printf("  Region:    %s\n", region)
	fmt.Printf("  Issuer:    %s\n", issuerURL)
	fmt.Printf("  Audiences: %v\n", audiences)

	// Create provider with logger
	logger := slog.Default()
	provider, err := awsfederation.NewProvider(ctx, region, logger)
	if err != nil {
		return fmt.Errorf("failed to create AWS provider: %w", err)
	}

	// Setup OIDC provider
	result, err := provider.Setup(ctx, federation.SetupConfig{
		IssuerURL: issuerURL,
		Audiences: audiences,
	})
	if err != nil {
		return fmt.Errorf("failed to setup OIDC provider: %w", err)
	}

	fmt.Printf("\n✓ AWS IAM OIDC Provider created successfully!\n")
	fmt.Printf("  ARN:        %s\n", result.ProviderARN)
	fmt.Printf("  Thumbprint: %s\n", result.Thumbprint)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Create IAM roles with trust policies referencing this provider\n")
	fmt.Printf("  2. Annotate your Kubernetes service accounts with the IAM role ARN\n")
	fmt.Printf("  3. Configure your pods to use the service account\n")

	return nil
}
