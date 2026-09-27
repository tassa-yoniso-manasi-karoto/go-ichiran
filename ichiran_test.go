package ichiran

import (
	"testing"
	"strings"

	"github.com/stretchr/testify/assert"
)


// TestBackwardCompatibilityAPI demonstrates the backward compatibility layer
func TestBackwardCompatibilityAPI(t *testing.T) {
	t.Skip("Skipping test that requires Docker container - run manually with ICHIRAN_MANUAL_TEST=1")
	
	// Initialize with the global functions
	err := InitQuiet()
	assert.NoError(t, err)
	
	// Clean up when done
	defer Close()
	
	// Test analysis with the global function
	result, err := Analyze("こんにちは")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	
	// Verify we got meaningful results
	assert.Greater(t, len(*result), 0, "Expected non-empty result")
	
	// Check if we got the expected token
	found := false
	for _, token := range *result {
		if token.Surface == "こんにちは" {
			found = true
			assert.Contains(t, strings.ToLower(token.Romaji), "konnichiha")
			break
		}
	}
	
	assert.True(t, found, "Expected to find token 'こんにちは' in results")
}

func TestSafe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal string",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "string with leading dash",
			input:    "-hello",
			expected: "hello",
		},
		{
			name:     "no leading dash",
			input:    "hello-world",
			expected: "hello-world",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safe(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}