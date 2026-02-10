package main

import (
	"github.com/spf13/cobra"
)

// newSetupCommand creates the setup parent command.
func newSetupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup OIDC identity federation with cloud providers",
		Long: `Setup commands for configuring OIDC identity federation with various cloud providers.

This command group helps you set up OIDC identity providers in your cloud environment
to enable secretless authentication from Kubernetes to cloud services.`,
		Example: `  # Setup AWS IAM OIDC Provider
  kube-iam-assume setup aws --issuer-url https://your-bucket.s3.amazonaws.com

  # Setup GCP Workload Identity Federation
  kube-iam-assume setup gcp --issuer-url https://storage.googleapis.com/your-bucket --project my-project`,
	}

	// Add subcommands from other files
	cmd.AddCommand(newAWSCommand())
	cmd.AddCommand(newGCPCommand())

	return cmd
}
