package traces

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/propagation"
)

func parseAllowlist(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var patterns []string
	for _, raw := range strings.Split(value, ",") {
		pattern := strings.ToLower(strings.TrimSpace(raw))
		host := strings.TrimPrefix(pattern, "*.")
		if !validDNSName(host) {
			return nil, errors.New("invalid trace propagation host pattern")
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func validDNSName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func (t *Traces) allows(host string) bool {
	host = strings.ToLower(host)
	for _, pattern := range t.allowlist {
		if strings.HasPrefix(pattern, "*.") {
			if strings.HasSuffix(host, pattern[1:]) && len(host) > len(pattern)-1 {
				return true
			}
		} else if host == pattern {
			return true
		}
	}
	return false
}

func (t *Traces) Extract(ctx context.Context, headers http.Header) context.Context {
	if t == nil {
		return ctx
	}
	// Baggage is opaque client data: never extract it into Vault's context.
	return propagation.TraceContext{}.Extract(ctx, propagation.HeaderCarrier(headers))
}

// Inject runs AFTER all credential injection/substitution, on every attempt.
// host is the canonical routing target, not the client Host header. A nil
// handle preserves existing forwarding, including during disabled tracing.
func (t *Traces) Inject(ctx context.Context, host string, headers http.Header) {
	if t == nil {
		return
	}
	allowed := t.allows(host)
	// Remove stale or credential-injected values, including malformed context.
	// Iterate for case-insensitive removal even with noncanonical map keys.
	for key := range headers {
		if strings.EqualFold(key, "traceparent") || strings.EqualFold(key, "tracestate") ||
			(!allowed && strings.EqualFold(key, "baggage")) {
			delete(headers, key)
		}
	}
	if allowed {
		propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(headers))
	}
}
