package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// expectedRunFlags is the single source of truth for flags both `vault run`
// and the top-level `run` shorthand must expose. Adding a flag to one without
// the other is a bug. `vault` is inherited from vaultCmd's persistent flags
// on `vault run` and registered locally on the top-level `run`.
var expectedRunFlags = []string{
	"address", "ttl", "vault",
	"isolation", "image", "mount", "keep", "no-firewall",
	"home-volume-shared", "share-agent-dir", "no-skills",
}

func TestRunFlagsRegistered(t *testing.T) {
	vCmd := findSubcommand(rootCmd, "vault")
	if vCmd == nil {
		t.Fatal("vault command not found")
	}
	rCmd := findSubcommand(vCmd, "run")
	if rCmd == nil {
		t.Fatal("vault run subcommand not found")
	}

	// Flag walks local + inherited persistent flags, matching what users see.
	for _, name := range expectedRunFlags {
		if rCmd.Flag(name) == nil {
			t.Errorf("expected vault run flag --%s to be registered", name)
		}
	}
}

// TestTopLevelRunRegistered guards the `agent-vault run` shorthand — it must
// be a direct child of rootCmd and expose the same flag surface as `vault run`.
func TestTopLevelRunRegistered(t *testing.T) {
	tCmd := findSubcommand(rootCmd, "run")
	if tCmd == nil {
		t.Fatal("top-level run command not found")
	}
	if tCmd.Parent() != rootCmd {
		t.Errorf("top-level run must be parented to rootCmd, got %v", tCmd.Parent())
	}

	for _, name := range expectedRunFlags {
		if tCmd.Flag(name) == nil {
			t.Errorf("expected top-level run flag --%s to be registered", name)
		}
	}
}

func TestMaybeInstallSkillsInstallsCompleteSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	maybeInstallSkills("Test Agent", ".test-agent")

	for path, marker := range map[string]string{
		filepath.Join(home, ".test-agent", "skills", "agent-vault-cli", "SKILL.md"):                   "# Agent Vault",
		filepath.Join(home, ".test-agent", "skills", "agent-vault-cli", "references", "proposals.md"): "# Proposals",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading installed skill file %s: %v", path, err)
		}
		if !strings.Contains(string(content), marker) {
			t.Errorf("installed skill file %s does not contain %q", path, marker)
		}
	}
}

func TestResolveNoSkills(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		args    []string
		want    bool
		wantErr string
	}{
		{name: "defaults false"},
		{name: "environment enables", env: "true", want: true},
		{name: "environment disables", env: "false"},
		{name: "explicit true overrides environment", env: "false", args: []string{"--no-skills"}, want: true},
		{name: "explicit false overrides environment", env: "true", args: []string{"--no-skills=false"}},
		{name: "invalid environment rejected", env: "sometimes", wantErr: "AGENT_VAULT_NO_SKILLS"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENT_VAULT_NO_SKILLS", tc.env)
			cmd := newRunCmdForTest()
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatalf("ParseFlags(%v): %v", tc.args, err)
			}

			got, err := resolveNoSkills(cmd)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resolveNoSkills() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveNoSkills(): %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveNoSkills() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestMaybeInstallSkillsIfEnabledNoSkillsPreservesFilesystem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	maybeInstallSkillsIfEnabled(true, "Test Agent", ".test-agent")

	if _, err := os.Stat(filepath.Join(home, ".test-agent")); !os.IsNotExist(err) {
		t.Fatalf("skill directory should not be created, stat error: %v", err)
	}
}

func TestAugmentEnvWithMITM_Disabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mitm/ca.pem" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("MITM proxy is not enabled on this server\n"))
	}))
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "mitm-ca.pem")
	baseEnv := []string{"FOO=bar"}

	env, port, ok, err := augmentEnvWithMITM(baseEnv, srv.URL, "av_sess_abc", "default", caPath)
	if err != nil {
		t.Fatalf("expected nil err on 404, got %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when server 404s")
	}
	if port != 0 {
		t.Errorf("expected port=0 when disabled, got %d", port)
	}
	if len(env) != len(baseEnv) || env[0] != "FOO=bar" {
		t.Errorf("env should be unchanged on 404, got %v", env)
	}
	if _, err := os.Stat(caPath); !os.IsNotExist(err) {
		t.Errorf("expected no CA file on 404, stat err=%v", err)
	}
}

func TestRequireMITMEnv_DisabledIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "mitm-ca.pem")
	_, _, err := requireMITMEnv(nil, srv.URL, "tok", "v", caPath)
	if err == nil {
		t.Fatal("expected fatal error when server has MITM disabled")
	}
	// Operator-facing message must point at the server-side fix.
	if !strings.Contains(err.Error(), "--mitm-port 0") {
		t.Errorf("error should reference --mitm-port 0; got: %v", err)
	}
}

func TestRequireMITMEnv_TransportFailureIsFatal(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "mitm-ca.pem")
	// Bogus address that will fail to dial.
	_, _, err := requireMITMEnv(nil, "http://127.0.0.1:1", "tok", "v", caPath)
	if err == nil {
		t.Fatal("expected fatal error on transport failure")
	}
	if !strings.Contains(err.Error(), "MITM setup failed") {
		t.Errorf("error should be wrapped with 'MITM setup failed'; got: %v", err)
	}
}

// fakeMITMServer returns an httptest server that mimics the real
// /v1/mitm/ca.pem endpoint. advertisedPort, when non-zero, is written
// into the X-MITM-Port response header.
func fakeMITMServer(t *testing.T, pem string, advertisedPort int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if advertisedPort > 0 {
			w.Header().Set("X-MITM-Port", fmt.Sprintf("%d", advertisedPort))
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		_, _ = w.Write([]byte(pem))
	}))
}

func stubSystemCATrust(t *testing.T) {
	t.Helper()
	previous := systemCAVerifier
	systemCAVerifier = func([]byte, string) error { return nil }
	t.Cleanup(func() { systemCAVerifier = previous })
}

func TestAugmentEnvWithMITM_Enabled(t *testing.T) {
	stubSystemCATrust(t)
	const fakePEM = "-----BEGIN CERTIFICATE-----\nMIIFAKE\n-----END CERTIFICATE-----\n"
	srv := fakeMITMServer(t, fakePEM, 9001)
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "mitm-ca.pem")
	env, port, ok, err := augmentEnvWithMITM(nil, srv.URL, "av_sess_abc", "default", caPath)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true on 200")
	}
	if port != 9001 {
		t.Errorf("port = %d, want 9001 (from X-MITM-Port header)", port)
	}

	got, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("reading CA file: %v", err)
	}
	if string(got) != fakePEM {
		t.Errorf("CA file contents mismatch:\nwant %q\n got %q", fakePEM, string(got))
	}

	want := map[string]string{
		"HTTPS_PROXY":         "", // checked separately below
		"https_proxy":         "", // checked separately below
		"HTTP_PROXY":          "", // checked separately below
		"http_proxy":          "", // checked separately below
		"NO_PROXY":            "", // checked separately — includes AV host
		"no_proxy":            "", // checked separately — matches NO_PROXY
		"NODE_USE_ENV_PROXY":  "1",
		"OPENCLAW_PROXY_URL":  "", // checked separately below (equals HTTPS_PROXY)
		"NODE_EXTRA_CA_CERTS": caPath,
		"UV_SYSTEM_CERTS":     "true",
	}
	vars := envMap(env)
	for k, v := range want {
		got, ok := vars[k]
		if !ok {
			t.Errorf("missing env var %s", k)
			continue
		}
		if v != "" && got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	// HTTP_PROXY and OPENCLAW_PROXY_URL must equal HTTPS_PROXY — all
	// point at the same MITM ingress so plain http:// upstreams and
	// OpenClaw's Proxyline route through the broker.
	if vars["HTTP_PROXY"] != vars["HTTPS_PROXY"] {
		t.Errorf("HTTP_PROXY = %q, want it to equal HTTPS_PROXY = %q", vars["HTTP_PROXY"], vars["HTTPS_PROXY"])
	}
	if vars["OPENCLAW_PROXY_URL"] != vars["HTTPS_PROXY"] {
		t.Errorf("OPENCLAW_PROXY_URL = %q, want it to equal HTTPS_PROXY = %q", vars["OPENCLAW_PROXY_URL"], vars["HTTPS_PROXY"])
	}
	if vars["https_proxy"] != vars["HTTPS_PROXY"] || vars["http_proxy"] != vars["HTTP_PROXY"] {
		t.Error("lowercase proxy variables must match their uppercase forms")
	}
	if vars["no_proxy"] != vars["NO_PROXY"] {
		t.Error("no_proxy must match NO_PROXY")
	}
	for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "DENO_CERT"} {
		if _, ok := vars[key]; ok {
			t.Errorf("host environment must not set replacement-style CA variable %s", key)
		}
	}

	// NO_PROXY must include the AV host so control-plane calls bypass the proxy.
	noProxy := vars["NO_PROXY"]
	if !strings.Contains(noProxy, "localhost") || !strings.Contains(noProxy, "127.0.0.1") {
		t.Errorf("NO_PROXY = %q, want localhost and 127.0.0.1", noProxy)
	}

	// Proxy URL must parse cleanly and carry token:vault userinfo.
	proxyURL := vars["HTTPS_PROXY"]
	if proxyURL == "" {
		t.Fatal("HTTPS_PROXY not set")
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse HTTPS_PROXY: %v", err)
	}
	if u.Scheme != "http" {
		t.Errorf("proxy scheme = %q, want http", u.Scheme)
	}
	if u.User == nil {
		t.Fatal("proxy URL missing userinfo")
	}
	if u.User.Username() != "av_sess_abc" {
		t.Errorf("proxy username = %q, want av_sess_abc", u.User.Username())
	}
	if pw, _ := u.User.Password(); pw != "default" {
		t.Errorf("proxy password (vault) = %q, want default", pw)
	}
	// Host should use the advertised X-MITM-Port (9001), not the compile-time
	// default — this guards the regression where --mitm-port 9000 produced
	// a URL pointing at 14322.
	wantHost := "127.0.0.1:9001"
	if u.Host != wantHost {
		t.Errorf("proxy host = %q, want %q", u.Host, wantHost)
	}
}

func TestAugmentEnvWithMITM_UsesAgentVaultHome(t *testing.T) {
	stubSystemCATrust(t)
	const fakePEM = "-----BEGIN CERTIFICATE-----\nMIIFAKE\n-----END CERTIFICATE-----\n"
	srv := fakeMITMServer(t, fakePEM, 9001)
	defer srv.Close()

	dataDir := filepath.Join(t.TempDir(), "custom-data")
	t.Setenv("AGENT_VAULT_HOME", dataDir)
	t.Setenv("HOME", t.TempDir())

	_, _, ok, err := augmentEnvWithMITM(nil, srv.URL, "av_sess_abc", "default", "")
	if err != nil {
		t.Fatalf("augmentEnvWithMITM: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true on 200")
	}
	got, err := os.ReadFile(filepath.Join(dataDir, "mitm-ca.pem"))
	if err != nil {
		t.Fatalf("reading CA file: %v", err)
	}
	if string(got) != fakePEM {
		t.Fatalf("CA file contents = %q, want %q", got, fakePEM)
	}
}

// TestAugmentEnvWithMITM_PortFallback verifies that a server which does
// not advertise X-MITM-Port (e.g. pre-v0.8 build) is still usable — the
// client falls back to DefaultMITMPort rather than emitting a URL with
// port 0.
func TestAugmentEnvWithMITM_PortFallback(t *testing.T) {
	stubSystemCATrust(t)
	const fakePEM = "-----BEGIN CERTIFICATE-----\nMIIFAKE\n-----END CERTIFICATE-----\n"
	srv := fakeMITMServer(t, fakePEM, 0) // no port header
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "mitm-ca.pem")
	_, port, ok, err := augmentEnvWithMITM(nil, srv.URL, "tok", "v", caPath)
	if err != nil || !ok {
		t.Fatalf("augmentEnvWithMITM: ok=%v err=%v", ok, err)
	}
	if port != DefaultMITMPort {
		t.Errorf("port = %d, want fallback to DefaultMITMPort (%d)", port, DefaultMITMPort)
	}
}

// TestAugmentEnvWithMITM_DedupesParentEnv guards the corporate-proxy
// regression: if the parent shell already has HTTPS_PROXY / SSL_CERT_FILE
// etc. set, C tooling (curl, libcurl-backed Python, git) reads the FIRST
// matching envp entry via getenv — so the stale parent value would win
// over the injected MITM value and bypass credential injection entirely.
// The fix strips the parent entries before appending the new ones.
func TestAugmentEnvWithMITM_DedupesParentEnv(t *testing.T) {
	stubSystemCATrust(t)
	const fakePEM = "-----BEGIN CERTIFICATE-----\nMIIFAKE\n-----END CERTIFICATE-----\n"
	srv := fakeMITMServer(t, fakePEM, 14322)
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "mitm-ca.pem")
	parentEnv := []string{
		"FOO=bar",
		"HTTPS_PROXY=http://corp-proxy:3128",
		"HTTP_PROXY=http://corp-proxy:3128",
		"https_proxy=http://lower-proxy:3128",
		"http_proxy=http://lower-proxy:3128",
		"NO_PROXY=internal.example.com",
		"no_proxy=metadata.internal",
		"SSL_CERT_FILE=/etc/ssl/corp-ca.pem",
		"NODE_EXTRA_CA_CERTS=/etc/ssl/corp-ca.pem",
		"REQUESTS_CA_BUNDLE=/etc/ssl/corp-ca.pem",
		"CURL_CA_BUNDLE=/etc/ssl/corp-ca.pem",
		"GIT_SSL_CAINFO=/etc/ssl/corp-ca.pem",
		"DENO_CERT=/etc/ssl/corp-ca.pem",
		"UNRELATED=keep-me",
	}
	env, _, ok, err := augmentEnvWithMITM(parentEnv, srv.URL, "tok", "v", caPath)
	if err != nil || !ok {
		t.Fatalf("augmentEnvWithMITM: ok=%v err=%v", ok, err)
	}

	// Each managed key must appear exactly once, and that single value
	// must be the injected MITM value — not the stale parent value.
	counts := map[string]int{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			counts[kv[:i]]++
		}
	}
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy", "NODE_USE_ENV_PROXY", "OPENCLAW_PROXY_URL", "UV_SYSTEM_CERTS", "NODE_EXTRA_CA_CERTS"} {
		if counts[k] != 1 {
			t.Errorf("%s appears %d times in env, want exactly 1 (POSIX getenv returns first match)", k, counts[k])
		}
	}

	vars := envMap(env)
	if vars["HTTPS_PROXY"] == "http://corp-proxy:3128" {
		t.Error("HTTPS_PROXY still carries the parent corp-proxy value")
	}
	if !strings.Contains(vars["HTTPS_PROXY"], "127.0.0.1:14322") {
		t.Errorf("HTTPS_PROXY = %q, want the MITM URL", vars["HTTPS_PROXY"])
	}
	for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "DENO_CERT"} {
		if _, ok := vars[key]; ok {
			t.Errorf("replacement-style CA variable %s survived host setup", key)
		}
	}
	if !strings.Contains(vars["NO_PROXY"], "internal.example.com") || !strings.Contains(vars["NO_PROXY"], "metadata.internal") {
		t.Errorf("NO_PROXY = %q, want parent uppercase and lowercase entries preserved", vars["NO_PROXY"])
	}
	if vars["UNRELATED"] != "keep-me" {
		t.Error("unrelated parent env vars must be preserved")
	}
	if vars["FOO"] != "bar" {
		t.Error("unrelated parent env vars must be preserved")
	}
}

func TestAugmentEnvWithMITM_SystemCANotTrusted(t *testing.T) {
	previous := systemCAVerifier
	systemCAVerifier = func([]byte, string) error { return fmt.Errorf("unknown authority") }
	t.Cleanup(func() { systemCAVerifier = previous })

	srv := fakeMITMServer(t, "-----BEGIN CERTIFICATE-----\nMIIFAKE\n-----END CERTIFICATE-----\n", 14322)
	defer srv.Close()

	_, _, _, err := augmentEnvWithMITM(nil, srv.URL, "tok", "v", filepath.Join(t.TempDir(), "ca.pem"))
	if err == nil {
		t.Fatal("expected host setup to fail when the CA is not system-trusted")
	}
	if !strings.Contains(err.Error(), "ca install-script") || !strings.Contains(err.Error(), "--isolation=container") {
		t.Fatalf("error must include both remediation paths, got: %v", err)
	}
}

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// newRunCmdForTest builds a run command with --vault registered locally so
// tests don't depend on the persistent flag inherited from vaultCmd.
func newRunCmdForTest() *cobra.Command {
	c := newRunCmd("test")
	c.Flags().String("vault", "", "target vault")
	return c
}

func TestResolveVaultForAgentMode(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("AGENT_VAULT_VAULT", "env-vault")
		c := newRunCmdForTest()
		_ = c.Flags().Set("vault", "flag-vault")
		got, err := resolveVaultForAgentMode(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "flag-vault" {
			t.Errorf("got %q, want flag-vault", got)
		}
	})

	t.Run("env when no flag", func(t *testing.T) {
		t.Setenv("AGENT_VAULT_VAULT", "env-vault")
		c := newRunCmdForTest()
		got, err := resolveVaultForAgentMode(c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "env-vault" {
			t.Errorf("got %q, want env-vault", got)
		}
	})

	t.Run("error when neither set", func(t *testing.T) {
		t.Setenv("AGENT_VAULT_VAULT", "")
		c := newRunCmdForTest()
		_, err := resolveVaultForAgentMode(c)
		if err == nil {
			t.Fatal("expected error when no vault is configured")
		}
		if !strings.Contains(err.Error(), "AGENT_VAULT_VAULT") {
			t.Errorf("error should mention AGENT_VAULT_VAULT; got: %v", err)
		}
	})
}

// TestStripEnvKeys_AgentVaultInjectedKeys is the AGENT_VAULT_* analogue of
// TestAugmentEnvWithMITM_DedupesParentEnv. In agent mode the parent env is
// guaranteed to already carry AGENT_VAULT_TOKEN/ADDR/VAULT (that's how agent
// mode is detected), so without this strip the parent's stale value would
// silently win in the child via POSIX getenv first-match semantics — most
// dangerously, --vault would be overridden by a stale AGENT_VAULT_VAULT.
func TestStripEnvKeys_AgentVaultInjectedKeys(t *testing.T) {
	parent := []string{
		"AGENT_VAULT_ACTIVE=false",
		"AGENT_VAULT_TOKEN=stale-tok",
		"AGENT_VAULT_ADDR=https://stale.example/",
		"AGENT_VAULT_VAULT=stale-vault",
		"UNRELATED=keep-me",
	}
	stripped := stripEnvKeys(parent, agentVaultInjectedKeys)
	for _, kv := range stripped {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if _, dropped := agentVaultInjectedKeys[key]; dropped {
			t.Errorf("expected %q to be stripped from parent env, still present as %q", key, kv)
		}
	}
	if !contains(stripped, "UNRELATED=keep-me") {
		t.Error("unrelated parent env vars must be preserved")
	}
}

func TestBuildAgentVaultEnvMarksChildActive(t *testing.T) {
	vars := envMap(buildAgentVaultEnv("tok", "https://vault.example", "prod"))
	if vars["AGENT_VAULT_ACTIVE"] != "true" {
		t.Errorf("AGENT_VAULT_ACTIVE = %q, want true", vars["AGENT_VAULT_ACTIVE"])
	}
	if vars["AGENT_VAULT_TOKEN"] != "tok" || vars["AGENT_VAULT_ADDR"] != "https://vault.example" || vars["AGENT_VAULT_VAULT"] != "prod" {
		t.Errorf("unexpected Agent Vault child env: %#v", vars)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestRunCmdAgentMode_RejectsTTL exercises the runCmdRunE early-exit when a
// pre-supplied token is used together with --ttl. The token's lifetime is
// fixed at mint time, so --ttl is meaningless in agent mode.
func TestRunCmdAgentMode_RejectsTTL(t *testing.T) {
	t.Setenv("AGENT_VAULT_TOKEN", "tok123")
	t.Setenv("AGENT_VAULT_ADDR", "http://example.invalid")
	t.Setenv("AGENT_VAULT_VAULT", "myvault")

	c := newRunCmd("test")
	_ = c.Flags().Set("ttl", "3600")
	err := runCmdRunE(c, []string{"true"})
	if err == nil {
		t.Fatal("expected error rejecting --ttl in agent mode")
	}
	if !strings.Contains(err.Error(), "--ttl has no effect") {
		t.Errorf("error should mention --ttl rejection; got: %v", err)
	}
}

// fakeServicesServer serves GET /v1/vaults/{name}/services with the
// given services JSON, mimicking the real handler's envelope.
func fakeServicesServer(t *testing.T, servicesJSON string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/vaults/") || !strings.HasSuffix(r.URL.Path, "/services") {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"vault":"default","services":%s}`, servicesJSON)
	}))
}

func TestFetchServicePlaceholders(t *testing.T) {
	srv := fakeServicesServer(t, `[
		{"name":"github","host":"api.github.com","auth":{"type":"passthrough"},
		 "substitutions":[{"key":"GITHUB_TOKEN","placeholder":"github_pat_thisisaplaceholder","env":"GITHUB_TOKEN","in":["header"]}]},
		{"name":"disabled-svc","host":"example.com","enabled":false,"auth":{"type":"passthrough"},
		 "substitutions":[{"key":"SHOULD_NOT_APPEAR","placeholder":"__nope__"}]},
		{"name":"no-subs","host":"api.stripe.com","auth":{"type":"bearer","token":"STRIPE_SECRET_KEY"}}
	]`)
	defer srv.Close()

	got, err := fetchServicePlaceholders(srv.URL, "av_sess_abc", "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 placeholder, got %v", got)
	}
	if got["GITHUB_TOKEN"] != "github_pat_thisisaplaceholder" {
		t.Errorf("GITHUB_TOKEN = %q, want github_pat_thisisaplaceholder", got["GITHUB_TOKEN"])
	}
}

func TestFetchServicePlaceholders_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := fetchServicePlaceholders(srv.URL, "av_sess_abc", "default"); err == nil {
		t.Fatal("expected error on server 500")
	}
}

func TestAugmentEnvWithPlaceholders_ScrubsParentEnv(t *testing.T) {
	srv := fakeServicesServer(t, `[
		{"name":"github","host":"api.github.com","auth":{"type":"passthrough"},
		 "substitutions":[{"key":"GITHUB_TOKEN","placeholder":"github_pat_thisisaplaceholder","env":"GITHUB_TOKEN","in":["header"]}]}
	]`)
	defer srv.Close()

	parent := []string{"GITHUB_TOKEN=real_leak", "UNRELATED=keepme"}
	env, err := augmentEnvWithPlaceholders(parent, srv.URL, "av_sess_abc", "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	vars := envMap(env)
	if vars["GITHUB_TOKEN"] != "github_pat_thisisaplaceholder" {
		t.Errorf("GITHUB_TOKEN = %q, want the placeholder (parent real value must be scrubbed)", vars["GITHUB_TOKEN"])
	}
	if vars["UNRELATED"] != "keepme" {
		t.Errorf("UNRELATED = %q, want keepme (unrelated env must survive)", vars["UNRELATED"])
	}
}

func TestAugmentEnvWithPlaceholders_NoSubstitutions(t *testing.T) {
	srv := fakeServicesServer(t, `[]`)
	defer srv.Close()

	parent := []string{"GITHUB_TOKEN=real_value_stays"}
	env, err := augmentEnvWithPlaceholders(parent, srv.URL, "av_sess_abc", "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(env) != 1 || env[0] != "GITHUB_TOKEN=real_value_stays" {
		t.Errorf("env should be unchanged when no substitutions exist, got %v", env)
	}
}

func TestAugmentEnvWithPlaceholders_NoEnvConfig(t *testing.T) {
	srv := fakeServicesServer(t, `[
		{"name":"github","host":"api.github.com","auth":{"type":"passthrough"},
		 "substitutions":[{"key":"GITHUB_TOKEN","placeholder":"github_pat_thisisaplaceholder","in":["header"]}]}
	]`)
	defer srv.Close()

	want := []string{"GITHUB_TOKEN=parent_value", "UNRELATED=keepme"}
	env, err := augmentEnvWithPlaceholders(slices.Clone(want), srv.URL, "av_sess_abc", "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !slices.Equal(env, want) {
		t.Errorf("env should be unchanged when substitutions do not configure env injection, got %v", env)
	}
}

func TestAugmentEnvWithPlaceholders_ExplicitEnv(t *testing.T) {
	srv := fakeServicesServer(t, `[
		{"name":"github","host":"api.github.com","auth":{"type":"passthrough"},
		 "substitutions":[{"key":"ACME_GH_TOKEN","placeholder":"github_pat_thisisaplaceholder","env":"GITHUB_TOKEN","in":["header"]}]}
	]`)
	defer srv.Close()

	// The parent env holds real values under BOTH names; only the configured
	// name is scrubbed and injected. The credential key itself is not an
	// env var, so ACME_GH_TOKEN passes through untouched.
	parent := []string{"GITHUB_TOKEN=real_leak", "ACME_GH_TOKEN=also_stays"}
	env, err := augmentEnvWithPlaceholders(parent, srv.URL, "av_sess_abc", "default")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	vars := envMap(env)
	if vars["GITHUB_TOKEN"] != "github_pat_thisisaplaceholder" {
		t.Errorf("GITHUB_TOKEN = %q, want the placeholder injected under the configured env name", vars["GITHUB_TOKEN"])
	}
	if vars["ACME_GH_TOKEN"] != "also_stays" {
		t.Errorf("ACME_GH_TOKEN = %q, want also_stays (credential key is not used for env injection)", vars["ACME_GH_TOKEN"])
	}
}
