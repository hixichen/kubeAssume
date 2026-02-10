package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/hixichen/kube-iam-assume/pkg/federation"
	"github.com/hixichen/kube-iam-assume/pkg/federation/gcp"
)

// newGCPCommand creates the GCP setup subcommand.
func newGCPCommand() *cobra.Command {
	var (
		issuerURL string
		projectID string
		poolID    string
		poolName  string
		audience  []string
	)

	cmd := &cobra.Command{
		Use:   "gcp",
		Short: "Setup GCP Workload Identity Federation",
		Long: `Setup GCP Workload Identity Federation for OIDC identity federation.

This command creates a Workload Identity Pool and OIDC provider in GCP
that trusts your Kubernetes cluster's OIDC issuer, enabling pods to
impersonate GCP service accounts using Kubernetes service account tokens.`,
		Example: `  # Basic setup
  kubeassume setup gcp --issuer-url https://storage.googleapis.com/my-bucket --project my-project

  # Setup with custom pool ID
  kubeassume setup gcp \
    --issuer-url https://storage.googleapis.com/my-bucket \
    --project my-project \
    --pool-id my-k8s-pool \
    --pool-name "My Kubernetes Pool"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGCPSetup(cmd.Context(), issuerURL, projectID, poolID, poolName, audience)
		},
	}

	cmd.Flags().StringVar(&issuerURL, "issuer-url", "", "OIDC issuer URL (required)")
	cmd.Flags().StringVar(&projectID, "project", "", "GCP project ID (required, or use GOOGLE_CLOUD_PROJECT env var)")
	cmd.Flags().StringVar(&poolID, "pool-id", "", "Workload Identity Pool ID (optional, auto-generated)")
	cmd.Flags().StringVar(&poolName, "pool-name", "", "Workload Identity Pool display name (optional)")
	cmd.Flags().StringArrayVar(&audience, "audience", []string{}, "OIDC audience(s)")

	if err := cmd.MarkFlagRequired("issuer-url"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("project"); err != nil {
		panic(err)
	}

	return cmd
}

func runGCPSetup(ctx context.Context, issuerURL, projectID, poolID, poolName string, audiences []string) error {
	fmt.Printf("Setting up GCP Workload Identity Federation...\n")
	fmt.Printf("  Project:   %s\n", projectID)
	fmt.Printf("  Issuer:    %s\n", issuerURL)
	if poolID != "" {
		fmt.Printf("  Pool ID:   %s\n", poolID)
	}
	fmt.Printf("  Audiences: %v\n", audiences)

	// Create provider with logger
	logger := slog.Default()
	provider, err := gcp.NewProvider(ctx, projectID, logger)
	if err != nil {
		return fmt.Errorf("failed to create GCP provider: %w", err)
	}

	// Setup Workload Identity Federation
	options := make(map[string]interface{})
	if poolID != "" {
		options["pool_id"] = poolID
	}
	if poolName != "" {
		options["pool_name"] = poolName
	}

	result, err := provider.Setup(ctx, federation.SetupConfig{
		IssuerURL: issuerURL,
		Audiences: audiences,
		Options:   options,
	})
	if err != nil {
		return fmt.Errorf("failed to setup Workload Identity Federation: %w", err)
	}

	fmt.Printf("\n✓ GCP Workload Identity Federation created successfully!\n")
	fmt.Printf("  Pool:      %s\n", result.ProviderARN)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Create service accounts in GCP for your workloads\n")
	fmt.Printf("  2. Grant the Workload Identity Pool access to those service accounts\n")
	fmt.Printf("  3. Configure Kubernetes service accounts with the GCP service account email\n")
	fmt.Printf("  4. Use the Workload Identity Pool to obtain GCP access tokens\n")

	return nil
}
