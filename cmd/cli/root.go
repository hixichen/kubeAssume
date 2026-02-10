package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// Version is set at build time.
	Version = "dev"
	// GitCommit is set at build time.
	GitCommit = "unknown"
)

// rootCmd is the root command for the kube-iam-assume CLI.
var rootCmd = &cobra.Command{
	Use:   "kube-iam-assume",
	Short: "kube-iam-assume enables secretless cloud access for Kubernetes",
	Long: `kube-iam-assume is a Kubernetes controller that enables secretless cloud access
for self-hosted Kubernetes clusters by publishing OIDC discovery metadata publicly,
bridging the gap between K8s service account tokens and cloud provider identity federation.`,
}

// Execute executes the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(newSetupCommand())
	rootCmd.AddCommand(NewStatusCommand())
	rootCmd.AddCommand(NewBucketCommand())
	rootCmd.AddCommand(versionCmd)
}
