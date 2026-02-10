package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of kube-iam-assume",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("kube-iam-assume version %s (commit: %s)\n", Version, GitCommit)
	},
}
