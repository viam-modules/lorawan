package regions

import (
	"testing"

	"go.viam.com/test"
)

func TestGetRegion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Region
	}{
		{
			name:     "US region full name",
			input:    "US915",
			expected: US,
		},
		{
			name:     "US region short name",
			input:    "US",
			expected: US,
		},
		{
			name:     "US region frequency only",
			input:    "915",
			expected: US,
		},
		{
			name:     "EU region full name",
			input:    "EU868",
			expected: EU,
		},
		{
			name:     "EU region short name",
			input:    "EU",
			expected: EU,
		},
		{
			name:     "EU region frequency only",
			input:    "868",
			expected: EU,
		},
		{
			name:     "lowercase input",
			input:    "eu868",
			expected: EU,
		},
		{
			name:     "invalid region",
			input:    "INVALID",
			expected: Unspecified,
		},
		{
			name:     "empty string",
			input:    "",
			expected: Unspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRegion(tt.input)
			test.That(t, result, test.ShouldEqual, tt.expected)
		})
	}
}
