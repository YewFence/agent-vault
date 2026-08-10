// Package isolation builds the non-cooperative container that
// `vault run --isolation=container` launches the child agent inside.
package isolation

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	ContainerCAPath       = "/etc/agent-vault/ca.pem"
	ContainerSystemCAPath = "/etc/ssl/certs/ca-certificates.crt"
	ContainerProxyHost    = "host.docker.internal"
	ContainerClaudeHome   = "/home/claude/.claude"
	ContainerClaudeConfig = "/home/claude/.claude.json"
)

// ProxyEnvParams feeds BuildProxyEnv for host and container processes.
type ProxyEnvParams struct {
	Host    string // MITM listener host from the child's point of view
	Port    int
	Token   string
	Vault   string
	CAPath  string // optional additional CA PEM for clients with append semantics
	NoProxy string // existing NO_PROXY entries to preserve
}

// BuildProxyEnv returns the proxy env vars and CA settings that are safe for
// a host process. Replacement-style CA variables are deliberately absent:
// the host's native trust store must remain available to the child.
// Canonical source for both the process path (augmentEnvWithMITM) and
// the container path (BuildContainerEnv) so the list can't drift.
//
// HTTPS_PROXY and HTTP_PROXY both point at the same proxy URL so the
// client uses the same listener for https:// and http:// upstreams;
// the proxy listener accepts CONNECT and absolute-form forward-proxy
// requests on the same port.
//
// NB: keep in sync with buildProxyEnv() in
// sdks/sdk-typescript/src/resources/sessions.ts.
func BuildProxyEnv(p ProxyEnvParams) []string {
	scheme := "http"
	proxyURL := (&url.URL{
		Scheme: scheme,
		User:   url.UserPassword(p.Token, p.Vault),
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}).String()
	env := []string{
		"HTTPS_PROXY=" + proxyURL,
		"https_proxy=" + proxyURL,
		"HTTP_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL,
		"NO_PROXY=" + mergeNoProxy(p.NoProxy, p.Host),
		"no_proxy=" + mergeNoProxy(p.NoProxy, p.Host),
		"NODE_USE_ENV_PROXY=1",
		"OPENCLAW_PROXY_URL=" + proxyURL,
		"UV_SYSTEM_CERTS=true",
	}
	if p.CAPath != "" {
		env = append(env, "NODE_EXTRA_CA_CERTS="+p.CAPath)
	}
	return env
}

func mergeNoProxy(existing, host string) string {
	items := []string{"localhost", "127.0.0.1"}
	if host != "" {
		items = append(items, host)
	}
	items = append(items, strings.Split(existing, ",")...)

	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return strings.Join(out, ",")
}

// ProxyEnvKeys are the keys BuildProxyEnv emits. POSIX getenv returns
// the first match in C code paths, so parent-env occurrences must be
// stripped before appending these.
var ProxyEnvKeys = []string{
	"HTTPS_PROXY",
	"https_proxy",
	"HTTP_PROXY",
	"http_proxy",
	"NO_PROXY",
	"no_proxy",
	"NODE_USE_ENV_PROXY",
	"OPENCLAW_PROXY_URL",
	"UV_SYSTEM_CERTS",
	"SSL_CERT_FILE",
	"NODE_EXTRA_CA_CERTS",
	"REQUESTS_CA_BUNDLE",
	"CURL_CA_BUNDLE",
	"GIT_SSL_CAINFO",
	"DENO_CERT",
}

// BuildContainerEnv returns the KEY=VALUE entries to pass to `docker
// run` via -e flags. Produces a fresh list rather than augmenting
// os.Environ() — the container should not inherit the host's env.
func BuildContainerEnv(token, vault string, httpPort, mitmPort int) []string {
	env := BuildProxyEnv(ProxyEnvParams{
		Host:   ContainerProxyHost,
		Port:   mitmPort,
		Token:  token,
		Vault:  vault,
		CAPath: ContainerCAPath,
	})
	// The entrypoint installs ContainerCAPath into the image's native trust
	// store before dropping privileges. File-oriented clients must therefore
	// use the generated system bundle, which retains public roots as well.
	env = append(env,
		"SSL_CERT_FILE="+ContainerSystemCAPath,
		"REQUESTS_CA_BUNDLE="+ContainerSystemCAPath,
		"CURL_CA_BUNDLE="+ContainerSystemCAPath,
		"GIT_SSL_CAINFO="+ContainerSystemCAPath,
		"DENO_CERT="+ContainerSystemCAPath,
	)
	return append(env,
		"AGENT_VAULT_TOKEN="+token,
		"AGENT_VAULT_ADDR="+fmt.Sprintf("http://%s:%d", ContainerProxyHost, httpPort),
		"AGENT_VAULT_VAULT="+vault,
		fmt.Sprintf("VAULT_HTTP_PORT=%d", httpPort),
		fmt.Sprintf("VAULT_MITM_PORT=%d", mitmPort),
	)
}
