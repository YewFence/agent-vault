package mitm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
)

// upstreamFailure is the client-facing classification of an upstream
// transport failure. Reason is a short human-readable sentence that
// writeUpstreamFailure combines with the target address.
type upstreamFailure struct {
	Code   string
	Reason string
}

// classifyUpstreamError maps a dial, TLS, or timeout failure from the
// upstream transport onto a stable error code used in proxy responses,
// request-log rows, and traces. The raw error must never reach the
// agent: x509 errors embed upstream certificate metadata (e.g. the full
// SAN list) that the agent cannot observe through the MITM termination,
// and transport errors may carry resolver internals. Callers log the
// raw error server-side instead.
func classifyUpstreamError(err error) upstreamFailure {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return upstreamFailure{Code: "dns_error", Reason: "Could not resolve the upstream host"}
	}

	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return upstreamFailure{Code: "tls_hostname_mismatch", Reason: "The upstream TLS certificate is not valid for the requested host (missing DNS name or IP SAN)"}
	}

	var authErr x509.UnknownAuthorityError
	if errors.As(err, &authErr) {
		return upstreamFailure{Code: "tls_unknown_authority", Reason: "The upstream TLS certificate is not trusted (self-signed or signed by an unknown CA)"}
	}

	var invalidErr x509.CertificateInvalidError
	if errors.As(err, &invalidErr) {
		if invalidErr.Reason == x509.Expired {
			return upstreamFailure{Code: "tls_certificate_invalid", Reason: "The upstream TLS certificate has expired"}
		}
		return upstreamFailure{Code: "tls_certificate_invalid", Reason: "The upstream TLS certificate is not valid"}
	}

	var recordErr tls.RecordHeaderError
	if errors.As(err, &recordErr) {
		return upstreamFailure{Code: "tls_handshake_failed", Reason: "The TLS handshake with the upstream failed"}
	}

	// Remote TLS alerts surface as the unexported tls.alert type (the
	// exported tls.AlertError is only wrapped on the QUIC path), so the
	// stable error strings from crypto/tls are the only handle.
	if strings.Contains(err.Error(), "tls: certificate required") {
		return upstreamFailure{Code: "tls_client_cert_required", Reason: "The upstream requires a TLS client certificate (mTLS)"}
	}
	if strings.Contains(err.Error(), "remote error: tls:") {
		return upstreamFailure{Code: "tls_handshake_failed", Reason: "The TLS handshake with the upstream failed"}
	}

	if isTimeoutError(err) {
		if strings.Contains(err.Error(), "TLS handshake timeout") {
			// net/http's tlsHandshakeTimeoutError has no exported
			// sentinel; its stable message is the only handle.
			return upstreamFailure{Code: "tls_handshake_failed", Reason: "Timed out during the TLS handshake with the upstream"}
		}
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return upstreamFailure{Code: "dial_timeout", Reason: "Timed out connecting to the upstream"}
		}
		return upstreamFailure{Code: "upstream_timeout", Reason: "Timed out waiting for the upstream to respond"}
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return upstreamFailure{Code: "connection_refused", Reason: "The upstream refused the connection (nothing listening or firewall rejected it)"}
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return upstreamFailure{Code: "network_unreachable", Reason: "The upstream network is unreachable"}
	}

	return upstreamFailure{Code: "upstream_error", Reason: "the proxy could not reach the upstream service"}
}

// isTimeoutError covers the three timeout shapes the transports produce:
// cancelled request contexts (context.DeadlineExceeded), connection
// deadlines (os.ErrDeadlineExceeded), and net/http's unexported error
// values, which only advertise themselves via net.Error.
func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
