package iface

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublisherType_String(t *testing.T) {
	tests := []struct {
		name        string
		pubType     PublisherType
		expectedStr string
	}{
		{
			name:        "S3 type",
			pubType:     PublisherTypeS3,
			expectedStr: "s3",
		},
		{
			name:        "GCS type",
			pubType:     PublisherTypeGCS,
			expectedStr: "gcs",
		},
		{
			name:        "Azure type",
			pubType:     PublisherTypeAzure,
			expectedStr: "azure",
		},
		{
			name:        "OCI type",
			pubType:     PublisherTypeOCI,
			expectedStr: "oci",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedStr, string(tt.pubType))
		})
	}
}

func TestPublisherTypeConstants(t *testing.T) {
	// Verify all constants are defined correctly
	assert.Equal(t, PublisherType("s3"), PublisherTypeS3)
	assert.Equal(t, PublisherType("gcs"), PublisherTypeGCS)
	assert.Equal(t, PublisherType("azure"), PublisherTypeAzure)
	assert.Equal(t, PublisherType("oci"), PublisherTypeOCI)
}

func TestPublisherType_ValidTypes(t *testing.T) {
	// Test that valid publisher types are recognized
	validTypes := []PublisherType{
		PublisherTypeS3,
		PublisherTypeGCS,
		PublisherTypeAzure,
		PublisherTypeOCI,
	}

	for _, pt := range validTypes {
		t.Run(string(pt), func(t *testing.T) {
			// Ensure the type is not empty
			assert.NotEmpty(t, string(pt))
			// Ensure the type matches expected value
			switch pt {
			case PublisherTypeS3:
				assert.Equal(t, "s3", string(pt))
			case PublisherTypeGCS:
				assert.Equal(t, "gcs", string(pt))
			case PublisherTypeAzure:
				assert.Equal(t, "azure", string(pt))
			case PublisherTypeOCI:
				assert.Equal(t, "oci", string(pt))
			}
		})
	}
}

func TestPublisherTypeFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected PublisherType
	}{
		{"s3", PublisherTypeS3},
		{"gcs", PublisherTypeGCS},
		{"azure", PublisherTypeAzure},
		{"oci", PublisherTypeOCI},
		{"unknown", PublisherType("unknown")},
		{"", PublisherType("")},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := PublisherType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
