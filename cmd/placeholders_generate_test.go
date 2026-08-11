package cmd

import (
	"strings"
	"testing"

	"github.com/Infisical/agent-vault/internal/broker"
)

func TestPlaceholdersGenerateRegistered(t *testing.T) {
	parent := findSubcommand(rootCmd, "placeholders")
	if parent == nil {
		t.Fatal("top-level placeholders command not found")
	}
	if c := findSubcommand(parent, "generate"); c == nil {
		t.Fatal("placeholders generate subcommand not found")
	}
}

func TestMaskCredential(t *testing.T) {
	t.Run("padded to key length", func(t *testing.T) {
		got := maskCredential("sk-ant-", 51)
		if len(got) != 51 {
			t.Fatalf("expected 51 chars, got %d (%q)", len(got), got)
		}
		base := "sk-ant-" + broker.PlaceholderMarker
		if !strings.HasPrefix(got, base) {
			t.Fatalf("expected prefix+marker, got %q", got)
		}
		if strings.TrimRight(got, "0") != base {
			t.Fatalf("expected zero padding after marker, got %q", got)
		}
	})

	t.Run("key shorter than marker is unpadded", func(t *testing.T) {
		want := "github_pat_" + broker.PlaceholderMarker
		if got := maskCredential("github_pat_", 10); got != want {
			t.Fatalf("expected bare prefix+marker %q, got %q", want, got)
		}
	})

	t.Run("exact length needs no padding", func(t *testing.T) {
		base := "sk-" + broker.PlaceholderMarker
		if got := maskCredential("sk-", len(base)); got != base {
			t.Fatalf("expected %q, got %q", base, got)
		}
	})

	t.Run("empty prefix yields bare marker", func(t *testing.T) {
		want := broker.PlaceholderMarker + strings.Repeat("0", 25-len(broker.PlaceholderMarker))
		if got := maskCredential("", 25); got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}

func TestSuggestPlaceholderPrefix(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{"anthropic", "sk-ant-api03-XXXX", "sk-ant-"},
		// "sk-or-" must win over the shorter "sk-" (OpenAI/DeepSeek).
		{"openrouter longest match", "sk-or-v1-XXXX", "sk-or-"},
		{"openai", "sk-proj-XXXX", "sk-"},
		{"github pat", "github_pat_11XXXX", "github_pat_"},
		{"gitlab", "glpat-XXXX", "glpat-"},
		{"slack", "xoxb-XXXX", "xoxb-"},
		{"unknown", "AKIAIOSFODNN7EXAMPLE", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := suggestPlaceholderPrefix(tc.key); got != tc.want {
				t.Fatalf("suggestPlaceholderPrefix(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}
