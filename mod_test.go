package modrank

import "testing"

func TestHostedRepository(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{
			name:     "gopkg.in/yaml.v3",
			expected: "github.com/go-yaml/yaml",
		},
		{
			name:     "go.lsp.dev/protocol",
			expected: "github.com/go-language-server/protocol",
		},
		{
			name:     "github.com/cncf/udpa/go",
			expected: "github.com/cncf/udpa",
		},
		{
			name:     "gopkg.in/jcmturner/gokrb5.v7",
			expected: "github.com/jcmturner/gokrb5",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := getHostedRepositoryByNameWithCache(test.name)
			if test.expected != got {
				t.Fatalf("failed to get hosted repository name from %s. got %s", test.name, got)
			}
		})
	}
}
