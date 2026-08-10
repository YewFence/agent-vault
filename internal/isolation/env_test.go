package isolation

import (
	"net/url"
	"strings"
	"testing"
)

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func TestBuildContainerEnv_ProxyURL(t *testing.T) {
	env := BuildContainerEnv("av_sess_abc", "myvault", 14321, 14322)
	vars := envMap(env)

	u, err := url.Parse(vars["HTTPS_PROXY"])
	if err != nil {
		t.Fatalf("parse HTTPS_PROXY: %v", err)
	}
	if u.Scheme != "http" {
		t.Errorf("scheme = %q, want http", u.Scheme)
	}
	if u.Hostname() != ContainerProxyHost {
		t.Errorf("host = %q, want %q (container view, not 127.0.0.1)", u.Hostname(), ContainerProxyHost)
	}
	if u.Port() != "14322" {
		t.Errorf("port = %q, want 14322", u.Port())
	}
	if u.User.Username() != "av_sess_abc" {
		t.Errorf("username = %q, want av_sess_abc", u.User.Username())
	}
	if pw, _ := u.User.Password(); pw != "myvault" {
		t.Errorf("password = %q, want myvault", pw)
	}
}

func TestBuildContainerEnv_CAPathsPreserveSystemRoots(t *testing.T) {
	env := BuildContainerEnv("tok", "v", 14321, 14322)
	vars := envMap(env)
	for _, k := range []string{
		"SSL_CERT_FILE",
		"REQUESTS_CA_BUNDLE",
		"CURL_CA_BUNDLE",
		"GIT_SSL_CAINFO",
		"DENO_CERT",
	} {
		if vars[k] != ContainerSystemCAPath {
			t.Errorf("%s = %q, want merged system bundle %q", k, vars[k], ContainerSystemCAPath)
		}
	}
	if vars["NODE_EXTRA_CA_CERTS"] != ContainerCAPath {
		t.Errorf("NODE_EXTRA_CA_CERTS = %q, want append-only CA path %q", vars["NODE_EXTRA_CA_CERTS"], ContainerCAPath)
	}
}

func TestBuildContainerEnv_AgentVaultAddrUsesContainerHost(t *testing.T) {
	env := BuildContainerEnv("tok", "v", 14321, 14322)
	vars := envMap(env)
	want := "http://" + ContainerProxyHost + ":14321"
	if vars["AGENT_VAULT_ADDR"] != want {
		t.Errorf("AGENT_VAULT_ADDR = %q, want %q", vars["AGENT_VAULT_ADDR"], want)
	}
	if vars["AGENT_VAULT_TOKEN"] != "tok" {
		t.Errorf("AGENT_VAULT_TOKEN = %q", vars["AGENT_VAULT_TOKEN"])
	}
	if vars["AGENT_VAULT_VAULT"] != "v" {
		t.Errorf("vault = %q", vars["AGENT_VAULT_VAULT"])
	}
}

func TestBuildContainerEnv_FirewallPortsEmitted(t *testing.T) {
	env := BuildContainerEnv("tok", "v", 14321, 14322)
	vars := envMap(env)
	if vars["VAULT_HTTP_PORT"] != "14321" {
		t.Errorf("VAULT_HTTP_PORT = %q", vars["VAULT_HTTP_PORT"])
	}
	if vars["VAULT_MITM_PORT"] != "14322" {
		t.Errorf("VAULT_MITM_PORT = %q", vars["VAULT_MITM_PORT"])
	}
}

func TestBuildProxyEnv_IPv6HostIsBracketed(t *testing.T) {
	env := BuildProxyEnv(ProxyEnvParams{
		Host:   "::1",
		Port:   14322,
		Token:  "tok",
		Vault:  "v",
		CAPath: "/tmp/ca.pem",
	})
	vars := envMap(env)
	u, err := url.Parse(vars["HTTPS_PROXY"])
	if err != nil {
		t.Fatalf("parse HTTPS_PROXY %q: %v", vars["HTTPS_PROXY"], err)
	}
	if u.Hostname() != "::1" {
		t.Errorf("hostname = %q, want ::1", u.Hostname())
	}
	if u.Port() != "14322" {
		t.Errorf("port = %q, want 14322", u.Port())
	}
	if !strings.Contains(vars["HTTPS_PROXY"], "[::1]:14322") {
		t.Errorf("HTTPS_PROXY = %q, want bracketed [::1]:14322 authority", vars["HTTPS_PROXY"])
	}
}

func TestBuildProxyEnv_HostUsesSystemTrust(t *testing.T) {
	env := BuildProxyEnv(ProxyEnvParams{
		Host:    "vault.example.test",
		Port:    14322,
		Token:   "tok",
		Vault:   "v",
		CAPath:  "/tmp/agent-vault.pem",
		NoProxy: "metadata.internal,localhost",
	})
	vars := envMap(env)
	for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "DENO_CERT"} {
		if _, ok := vars[key]; ok {
			t.Errorf("host proxy env unexpectedly contains %s", key)
		}
	}
	if vars["UV_SYSTEM_CERTS"] != "true" {
		t.Errorf("UV_SYSTEM_CERTS = %q", vars["UV_SYSTEM_CERTS"])
	}
	if vars["NODE_EXTRA_CA_CERTS"] != "/tmp/agent-vault.pem" {
		t.Errorf("NODE_EXTRA_CA_CERTS = %q", vars["NODE_EXTRA_CA_CERTS"])
	}
	if vars["NO_PROXY"] != "localhost,127.0.0.1,vault.example.test,metadata.internal" {
		t.Errorf("NO_PROXY = %q", vars["NO_PROXY"])
	}
	if vars["no_proxy"] != vars["NO_PROXY"] {
		t.Errorf("no_proxy = %q, want %q", vars["no_proxy"], vars["NO_PROXY"])
	}
}

// HTTP_PROXY mirrors HTTPS_PROXY: both point at the same MITM ingress
// so plain http:// upstreams route through the broker via absolute-form
// forward-proxy requests.
func TestBuildContainerEnv_HTTPProxyMatchesHTTPS(t *testing.T) {
	env := BuildContainerEnv("tok", "v", 14321, 14322)
	vars := envMap(env)
	if vars["HTTP_PROXY"] == "" {
		t.Fatal("HTTP_PROXY not set; expected to mirror HTTPS_PROXY")
	}
	if vars["HTTP_PROXY"] != vars["HTTPS_PROXY"] {
		t.Errorf("HTTP_PROXY = %q, want it to equal HTTPS_PROXY = %q", vars["HTTP_PROXY"], vars["HTTPS_PROXY"])
	}
}
