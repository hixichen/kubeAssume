package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// newGenerateCommand creates the generate-bucket-name command.
func newGenerateCommand() *cobra.Command {
	var (
		prefix  string
		region  string
		cluster string
		tags    []string
		output  string
	)

	cmd := &cobra.Command{
		Use:   "generate-bucket-name",
		Short: "Generate a new, non-guessable bucket name and create the bucket",
		Long: `Generates a unique and non-guessable bucket name using a prefix and a UUID,
creates the bucket in the specified cloud provider, and applies identifying tags.

This is the recommended way to create buckets for production use to prevent
untargeted bucket enumeration attacks.`,
		Example: `  # Generate a bucket name with a prefix and region for AWS S3
  kube-iam-assume generate-bucket-name --prefix oidc --region us-west-2 --cluster prod-us-west-2

  # Add custom tags
  kube-iam-assume generate-bucket-name --prefix oidc --region us-west-2 --cluster prod-us-west-2 --tags "team=platform,env=production"

  # Output as a Kubernetes ConfigMap
  kube-iam-assume generate-bucket-name --prefix oidc --region us-west-2 --cluster prod-us-west-2 --output configmap`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(cmd.Context(), prefix, region, cluster, tags, output)
		},
	}

	cmd.Flags().StringVar(&prefix, "prefix", "oidc", "A human-readable prefix for the bucket name")
	cmd.Flags().StringVar(&region, "region", "", "The cloud provider region for the bucket (e.g., us-west-2 for AWS)")
	cmd.Flags().StringVar(&cluster, "cluster", "", "A name for your cluster to use in tags")
	cmd.Flags().StringArrayVar(&tags, "tags", []string{}, "A list of custom tags to apply to the bucket (e.g., 'key1=value1,key2=value2')")
	cmd.Flags().StringVar(&output, "output", "text", "Output format (text, json, helm, configmap)")

	if err := cmd.MarkFlagRequired("region"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("cluster"); err != nil {
		panic(err)
	}

	return cmd
}

func runGenerate(ctx context.Context, prefix, region, cluster string, customTags []string, outputFormat string) error {
	// 1. Generate bucket name
	randomSuffix := uuid.New().String()
	bucketName := fmt.Sprintf("%s-%s", prefix, strings.ReplaceAll(randomSuffix, "-", "")[0:12])

	// For now, we only support AWS S3. This would be extended for other providers.
	issuerURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com", bucketName, region)

	fmt.Printf("Generated bucket name: %s\n", bucketName)
	fmt.Printf("Generated issuer URL: %s\n", issuerURL)

	// 2. Create S3 client (TODO: use a shared client factory)
	// For now, create a simple client. This assumes credentials are in the environment.
	// cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	// if err != nil {
	// 	return fmt.Errorf("failed to load AWS config: %w", err)
	// }
	// s3Client := s3.NewFromConfig(cfg)
	// s3Client := &s3.Client{} // Placeholder

	// 3. Create the bucket
	fmt.Printf("Creating S3 bucket: %s in region %s...\n", bucketName, region)
	// _, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
	// 	Bucket: &bucketName,
	// 	CreateBucketConfiguration: &types.CreateBucketConfiguration{
	// 		LocationConstraint: types.BucketLocationConstraint(region),
	// 	},
	// })
	// if err != nil {
	// 	return fmt.Errorf("failed to create bucket: %w", err)
	// }

	// 4. Apply tags
	allTags := map[string]string{
		"kube-iam-assume/managed-by": "kube-iam-assume",
		"kube-iam-assume/cluster":    cluster,
		"kube-iam-assume/prefix":     prefix,
		"kube-iam-assume/created-at": time.Now().UTC().Format(time.RFC3339),
		"kube-iam-assume/issuer-url": issuerURL,
	}
	for _, t := range customTags {
		parts := strings.SplitN(t, "=", 2)
		if len(parts) == 2 {
			allTags[parts[0]] = parts[1]
		}
	}

	fmt.Printf("Applying tags to bucket %s...\n", bucketName)
	// s3Tags := []types.Tag{}
	// for k, v := range allTags {
	// 	s3Tags = append(s3Tags, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	// }
	// _, err = s3Client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
	// 	Bucket: &bucketName,
	// 	Tagging: &types.Tagging{TagSet: s3Tags},
	// })
	// if err != nil {
	// 	return fmt.Errorf("failed to apply tags to bucket: %w", err)
	// }

	// 5. Format output
	switch outputFormat {
	case "text":
		fmt.Printf("\n--- Bucket Details ---\n")
		fmt.Printf("Bucket name:  %s\n", bucketName)
		fmt.Printf("Issuer URL:   %s\n", issuerURL)
		fmt.Printf("Region:       %s\n\n", region)
		fmt.Printf("Use this issuer URL in your API server configuration:\n")
		fmt.Printf("  --service-account-issuer=%s\n\n", issuerURL)
		fmt.Printf("And in your Helm install:\n")
		fmt.Printf("  --set config.publisher.s3.bucket=%s\n", bucketName)
		fmt.Printf("  --set config.publisher.s3.region=%s\n", region)
	case "json":
		// TODO
	case "helm":
		// TODO
	case "configmap":
		// TODO
	default:
		return fmt.Errorf("unsupported output format: %s", outputFormat)
	}

	fmt.Println("\nBucket creation and tagging logic is currently placeholder.")
	return nil
}
