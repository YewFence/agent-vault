// Package brokercore is the runtime glue powering Agent Vault's transparent
// MITM proxy ingress: credential resolution behind a CredentialProvider
// interface and session+vault resolution behind a SessionResolver interface.
//
// brokercore depends on broker, store, and crypto. broker stays a pure
// config library with no runtime coupling.
package brokercore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Infisical/agent-vault/internal/broker"
)

// DefaultMaxResponseBytes is the default cap for response bodies on the
// MITM proxy ingress. 0 means unlimited — response bodies are streamed
// with a small buffer so there is no OOM risk. Operators can set a cap
// via --max-response-bytes / AGENT_VAULT_MAX_RESPONSE_BYTES.
const DefaultMaxResponseBytes int64 = 0

// ProxyErrorHeader is the response header Agent Vault sets on broker-layer
// error responses so SDK clients can distinguish them from upstream
// responses that happen to share the same status code.
const ProxyErrorHeader = "X-Agent-Vault-Proxy-Error"

// HopByHopHeaders are HTTP/1.1 hop-by-hop headers that must not be
// forwarded by a proxy. Includes Proxy-Connection — non-RFC but emitted
// by some HTTP/1.0 clients and conventionally treated as hop-by-hop.
var HopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// IsHopByHop reports whether the given header name is hop-by-hop.
func IsHopByHop(name string) bool {
	return HopByHopHeaders[http.CanonicalHeaderKey(name)]
}

// IsValidHost reports whether h is a safe hostname for use in an outbound
// URL or CONNECT target. Rejects characters that could cause URL parsing
// issues (userinfo injection, path separators, whitespace, control chars)
// and enforces DNS length + no leading/trailing dot. h must NOT include a
// port — strip it first with net.SplitHostPort if applicable.
func IsValidHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, c := range h {
		if c == '@' || c == '?' || c == '#' || c == '/' || c == '\\' || c == ' ' || c == '%' || c < 0x20 || c == 0x7f {
			return false
		}
	}
	return !strings.HasPrefix(h, ".") && !strings.HasSuffix(h, ".")
}

// brokerScopedRequestHeaders are headers that authenticate the client to
// Agent Vault itself (Proxy-Authorization on the MITM proxy ingress —
// reaching it via either HTTPS_PROXY/CONNECT or HTTP_PROXY/absolute-form;
// X-Vault on the control-plane endpoints used by instance-level agent
// tokens, e.g. /discover and /v1/proposals). They must never traverse
// the broker → target hop.
var brokerScopedRequestHeaders = map[string]bool{
	"X-Vault":             true,
	"Proxy-Authorization": true,
}

// IsBrokerScopedRequestHeader reports whether a request header
// authenticates the client to Agent Vault itself and must be stripped
// before forwarding to the target service.
func IsBrokerScopedRequestHeader(name string) bool {
	return brokerScopedRequestHeaders[http.CanonicalHeaderKey(name)]
}

// ApplyInjection writes outbound headers for a resolved InjectResult.
// Client headers pass through except hop-by-hop, broker-scoped
// (Proxy-Authorization, X-Vault), the keys of inject.Headers, and any names in
// extraStrip (additional caller-specified headers). Pre-stripping
// inject.Headers keys is what preserves the "injected always wins"
// security invariant; it is not a perf shortcut. Per RFC 7230 §6.1,
// any header named in the client's Connection field is also hop-by-hop
// for that connection and is stripped.
func ApplyInjection(src, dst http.Header, inject *InjectResult, extraStrip ...string) {
	strip := make(map[string]bool, len(extraStrip)+len(inject.Headers))
	for _, s := range extraStrip {
		strip[http.CanonicalHeaderKey(s)] = true
	}
	for k := range inject.Headers {
		strip[http.CanonicalHeaderKey(k)] = true
	}
	for _, c := range src.Values("Connection") {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				strip[http.CanonicalHeaderKey(f)] = true
			}
		}
	}
	for k, vv := range src {
		ck := http.CanonicalHeaderKey(k)
		if IsHopByHop(ck) || IsBrokerScopedRequestHeader(ck) || strip[ck] {
			continue
		}
		for _, v := range vv {
			dst.Add(ck, v)
		}
	}
	for k, v := range inject.Headers {
		dst.Set(k, v)
	}
}

// helpLinks returns the standard "see available services / usage instructions"
// suffix appended to broker-layer error messages when baseURL is known.
func helpLinks(baseURL string) string {
	return fmt.Sprintf(
		"To see available services, GET %s/discover. For Agent Vault usage instructions, GET %s/v1/skills/agent-vault-cli/SKILL.md",
		baseURL, baseURL,
	)
}

// ForbiddenHintBody returns the JSON-shaped body for a 403 response on the
// MITM ingress when the target host is not matched by any broker service in
// the vault. baseURL is the externally-reachable control-plane URL used to
// build actionable help links so agents without the skill file pre-loaded
// can self-discover available services and usage instructions.
func ForbiddenHintBody(targetHost, vaultName, baseURL string) map[string]interface{} {
	body := map[string]interface{}{
		"error":   "forbidden",
		"message": fmt.Sprintf("No broker service matching host %q in vault %q", targetHost, vaultName),
		"proposal_hint": map[string]interface{}{
			"host":                 targetHost,
			"endpoint":             "POST /v1/proposals",
			"supported_auth_types": broker.SupportedAuthTypes,
		},
	}
	if baseURL != "" {
		body["help"] = "This request was intercepted by Agent Vault. " + helpLinks(baseURL)
	}
	return body
}

// ShouldStripResponseHeader reports whether an upstream response header
// must not be forwarded to the agent: hop-by-hop headers plus Set-Cookie.
// Stripping Set-Cookie prevents the upstream from planting cookies in the
// agent's jar.
func ShouldStripResponseHeader(name string) bool {
	return IsHopByHop(name) || strings.EqualFold(name, "Set-Cookie")
}

// WriteProxyError writes a JSON error response with Content-Type, the
// X-Agent-Vault-Proxy-Error header (so SDKs can distinguish broker-layer
// errors from upstream status codes that happen to match), and a
// {error, message} body.
func WriteProxyError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(ProxyErrorHeader, "true")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}

// writeProxyErrorWithHelp is like WriteProxyError but appends an optional
// help field when baseURL is non-empty.
func writeProxyErrorWithHelp(w http.ResponseWriter, status int, code, message, baseURL string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(ProxyErrorHeader, "true")
	w.WriteHeader(status)
	body := map[string]interface{}{"error": code, "message": message}
	if baseURL != "" {
		body["help"] = helpLinks(baseURL)
	}
	_ = json.NewEncoder(w).Encode(body)
}

// WriteForbiddenHint writes a 403 with the shared proposal_hint body for
// the "host not brokerable" case on the MITM ingress.
func WriteForbiddenHint(w http.ResponseWriter, targetHost, vaultName, baseURL string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(ProxyErrorHeader, "true")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(ForbiddenHintBody(targetHost, vaultName, baseURL))
}

// InjectErrorSemantics is the client and audit-log classification for a
// CredentialProvider.Inject failure. ResponseCode and LogCode differ only for
// unmatched hosts: clients receive "forbidden" with a proposal hint, while
// audit rows retain "no_match" for unmatched-host queries.
type InjectErrorSemantics struct {
	Status       int
	ResponseCode string
	LogCode      string
}

// ClassifyInjectError keeps proxy responses and request-log rows aligned.
func ClassifyInjectError(err error) InjectErrorSemantics {
	switch {
	case errors.Is(err, ErrServiceNotFound):
		return InjectErrorSemantics{Status: http.StatusForbidden, ResponseCode: "forbidden", LogCode: "no_match"}
	case errors.Is(err, ErrServiceDisabled):
		return InjectErrorSemantics{Status: http.StatusForbidden, ResponseCode: "service_disabled", LogCode: "service_disabled"}
	case errors.Is(err, ErrOAuthNotConnected):
		return InjectErrorSemantics{Status: http.StatusBadGateway, ResponseCode: "oauth_not_connected", LogCode: "oauth_not_connected"}
	case errors.Is(err, ErrOAuthRefreshFailed):
		return InjectErrorSemantics{Status: http.StatusBadGateway, ResponseCode: "oauth_refresh_failed", LogCode: "oauth_refresh_failed"}
	case errors.Is(err, ErrCredentialMissing):
		return InjectErrorSemantics{Status: http.StatusBadGateway, ResponseCode: "credential_not_found", LogCode: "credential_not_found"}
	default:
		return InjectErrorSemantics{Status: http.StatusInternalServerError, ResponseCode: "internal", LogCode: "internal"}
	}
}

// WriteInjectError maps a CredentialProvider.Inject error to the standard
// HTTP response on the MITM ingress. baseURL is the externally-reachable
// control-plane URL for help links. Callers that want to log before
// responding should do so before calling this helper.
func WriteInjectError(w http.ResponseWriter, err error, targetHost, vaultName, baseURL string) {
	semantics := ClassifyInjectError(err)
	switch {
	case errors.Is(err, ErrServiceNotFound):
		WriteForbiddenHint(w, targetHost, vaultName, baseURL)
	case errors.Is(err, ErrServiceDisabled):
		writeProxyErrorWithHelp(w, semantics.Status, semantics.ResponseCode,
			fmt.Sprintf("Broker service matching host %q in vault %q is currently disabled", targetHost, vaultName), baseURL)
	case errors.Is(err, ErrOAuthNotConnected):
		writeProxyErrorWithHelp(w, semantics.Status, semantics.ResponseCode,
			"OAuth credential is approved but not yet connected — complete the connection in the Agent Vault dashboard", baseURL)
	case errors.Is(err, ErrOAuthRefreshFailed):
		writeProxyErrorWithHelp(w, semantics.Status, semantics.ResponseCode,
			"OAuth token expired and refresh failed — reconnect in the Agent Vault dashboard", baseURL)
	case errors.Is(err, ErrCredentialMissing):
		writeProxyErrorWithHelp(w, semantics.Status, semantics.ResponseCode,
			"A required credential could not be resolved; check vault configuration", baseURL)
	default:
		WriteProxyError(w, semantics.Status, semantics.ResponseCode,
			"Failed to resolve broker services")
	}
}
