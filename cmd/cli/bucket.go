package main

import (
	"github.com/spf13/cobra"
)

// NewBucketCommand creates the bucket parent command.
func NewBucketCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bucket",
		Short: "Manage OIDC buckets",
		Long:  `Commands for managing OIDC buckets used for publishing discovery metadata.`,
	}

	cmd.AddCommand(newGenerateCommand())
	// Add other bucket-related commands here

	return cmd
}
