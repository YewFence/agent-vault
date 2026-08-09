package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Infisical/agent-vault/internal/session"
)

func TestPlaceholdersCommandRegistered(t *testing.T) {
	c := findSubcommand(rootCmd, "placeholders")
	if c == nil {
		t.Fatal("top-level placeholders command not found")
	}
	for _, name := range []string{"address", "vault"} {
		if c.Flag(name) == nil {
			t.Errorf("expected placeholders flag --%s to be registered", name)
		}
	}
	if c.Flag("shell") != nil {
		t.Error("placeholders should emit portable POSIX shell syntax without a --shell flag")
	}
}

func TestRenderPlaceholderScriptIsSortedAndShellSafe(t *testing.T) {
	placeholders := map[string]string{
		"Z_TOKEN": "dollar-$HOME",
		"A_TOKEN": "apostrophe'd",
	}
	want := "export A_TOKEN='apostrophe'\"'\"'d'\nexport Z_TOKEN='dollar-$HOME'\n"

	got, err := renderPlaceholderScript(placeholders)
	if err != nil {
		t.Fatalf("renderPlaceholderScript: %v", err)
	}
	if got != want {
		t.Fatalf("script = %q, want %q", got, want)
	}
}

func TestRenderPlaceholderScriptRejectsInvalidInput(t *testing.T) {
	if _, err := renderPlaceholderScript(map[string]string{"BAD;echo injected": "value"}); err == nil {
		t.Fatal("expected invalid environment name to be rejected")
	}
}

func TestRenderPlaceholderScriptEvaluatesValuesLiterally(t *testing.T) {
	const want = "apostrophe' dollar$ newline\nend"
	script, err := renderPlaceholderScript(map[string]string{"VALUE": want})
	if err != nil {
		t.Fatalf("renderPlaceholderScript: %v", err)
	}

	out, err := exec.Command("bash", "-c", script+`printf '%s' "$VALUE"`).Output()
	if err != nil {
		t.Fatalf("bash evaluating generated script: %v", err)
	}
	if got := string(out); got != want {
		t.Fatalf("evaluated value = %q, want %q", got, want)
	}
}

func TestPlaceholdersUsesEnvironmentTokenWithoutLogin(t *testing.T) {
	const (
		agentToken = "av_agt_environment"
		vault      = "project"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/vaults/"+vault+"/services" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+agentToken {
			t.Errorf("Authorization = %q, want environment token", got)
		}
		if got := r.Header.Get("X-Vault"); got != vault {
			t.Errorf("X-Vault = %q, want %q", got, vault)
		}
		fmt.Fprint(w, `{"services":[{"name":"github","host":"api.github.com","substitutions":[{"key":"ACME_GITHUB_TOKEN","env":"GITHUB_TOKEN","placeholder":"github_pat_placeholder","in":["header"]}]}]}`)
	}))
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENT_VAULT_TOKEN", agentToken)
	t.Setenv("AGENT_VAULT_ADDR", srv.URL)
	t.Setenv("AGENT_VAULT_VAULT", vault)

	c := newPlaceholdersCmd()
	var stdout, stderr bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&stderr)
	if err := c.Execute(); err != nil {
		t.Fatalf("placeholders: %v", err)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if got, want := stdout.String(), "export GITHUB_TOKEN='github_pat_placeholder'\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestFetchServicePlaceholdersRejectsConflictingEnvironmentNames(t *testing.T) {
	srv := fakeServicesServer(t, `[
		{"name":"svc-a","host":"a.example.com","auth":{"type":"passthrough"},
		 "substitutions":[{"key":"FIRST_KEY","placeholder":"__first__","env":"SHARED_KEY"}]},
		{"name":"svc-b","host":"b.example.com","auth":{"type":"passthrough"},
		 "substitutions":[{"key":"SECOND_KEY","placeholder":"__second__","env":"SHARED_KEY"}]}
	]`)
	defer srv.Close()

	_, err := fetchServicePlaceholders(srv.URL, "av_sess_abc", "default")
	if err == nil {
		t.Fatal("expected conflicting placeholders for one environment variable to fail")
	}
	for _, want := range []string{"SHARED_KEY", "svc-a", "svc-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestReferenceEnvironmentScript(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	agentVaultStub := filepath.Join(binDir, "agent-vault")
	stub := `#!/bin/sh
case "$1 $2" in
  "vault current") printf '%s\n' project ;;
  "vault token") printf '%s\n' av_sess_test ;;
  "ca fetch")
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "--output" ]; then printf '%s\n' TEST_CA > "$2"; exit; fi
      shift
    done
    exit 1
    ;;
  "placeholders ") printf '%s\n' "export TEST_PLACEHOLDER='placeholder-value'" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(agentVaultStub, []byte(stub), 0o755); err != nil {
		t.Fatalf("write agent-vault stub: %v", err)
	}

	scriptPath, err := filepath.Abs(filepath.Join("..", "examples", "agent-vault-env.sh"))
	if err != nil {
		t.Fatalf("resolve reference script: %v", err)
	}
	command := `. "$1"
printf '%s\n' "$AGENT_VAULT_TOKEN" "$AGENT_VAULT_VAULT" "$TEST_PLACEHOLDER" "$NO_PROXY"
if set | grep '^agent_vault_' >/dev/null; then exit 1; fi
`
	cmd := exec.Command("sh", "-c", command, "sh", scriptPath)
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + tempDir,
		"AGENT_VAULT_ADDR=http://vault.example.test:14321",
		"AGENT_VAULT_MITM_ADDR=vault.example.test:14322",
		"AGENT_VAULT_TOKEN=",
		"AGENT_VAULT_VAULT=",
		"NO_PROXY=metadata.internal",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("source reference script: %v\n%s", err, out)
	}
	want := "av_sess_test\nproject\nplaceholder-value\nmetadata.internal,localhost,127.0.0.1\n"
	if got := string(out); got != want {
		t.Fatalf("script output = %q, want %q", got, want)
	}
}

func TestPlaceholdersUsesLoginSessionWithoutMintingScopedToken(t *testing.T) {
	const (
		loginToken = "av_sess_login"
		vault      = "project"
	)

	var minted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/vaults/"+vault+"/services":
			if got := r.Header.Get("Authorization"); got != "Bearer "+loginToken {
				t.Errorf("Authorization = %q, want login session", got)
			}
			fmt.Fprint(w, `{"services":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			minted = true
			http.Error(w, "must not mint", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENT_VAULT_TOKEN", "")
	t.Setenv("AGENT_VAULT_ADDR", "")
	t.Setenv("AGENT_VAULT_VAULT", "")
	if err := session.Save(&session.ClientSession{Token: loginToken, Address: srv.URL}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	c := newPlaceholdersCmd()
	var stdout bytes.Buffer
	c.SetOut(&stdout)
	c.SetArgs([]string{"--vault", vault})
	if err := c.Execute(); err != nil {
		t.Fatalf("placeholders: %v", err)
	}
	if minted {
		t.Fatal("placeholders unexpectedly minted a scoped session")
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("stdout = %q, want empty script", stdout.String())
	}
}
