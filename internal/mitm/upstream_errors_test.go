package mitm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Infisical/agent-vault/internal/brokercore"
)

func TestClassifyUpstreamError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"dns nxdomain", &net.DNSError{Err: "no such host", Name: "nx.invalid", IsNotFound: true}, "dns_error"},
		{"dns timeout", &net.DNSError{Err: "i/o timeout", Name: "slow.invalid", IsTimeout: true}, "dns_error"},
		{"wrapped dns", fmt.Errorf("dial tcp: %w", &net.DNSError{Err: "no such host", Name: "nx.invalid", IsNotFound: true}), "dns_error"},
		{"hostname mismatch", fmt.Errorf("failed to verify certificate: %w", x509.HostnameError{Host: "10.0.0.1"}), "tls_hostname_mismatch"},
		{"unknown authority", fmt.Errorf("failed to verify certificate: %w", x509.UnknownAuthorityError{}), "tls_unknown_authority"},
		{"expired certificate", fmt.Errorf("failed to verify certificate: %w", x509.CertificateInvalidError{Reason: x509.Expired}), "tls_certificate_invalid"},
		{"certificate otherwise invalid", fmt.Errorf("failed to verify certificate: %w", x509.CertificateInvalidError{Reason: x509.CANotAuthorizedForThisName}), "tls_certificate_invalid"},
		{"client cert required", &net.OpError{Op: "remote error", Err: errors.New("tls: certificate required")}, "tls_client_cert_required"},
		{"other alert", &net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")}, "tls_handshake_failed"},
		{"record header", fmt.Errorf("handshake: %w", tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}), "tls_handshake_failed"},
		{"tls handshake timeout message", tlsHandshakeTimeoutLike{}, "tls_handshake_failed"},
		{"request context deadline", context.DeadlineExceeded, "upstream_timeout"},
		{"read deadline", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, "upstream_timeout"},
		{"dial deadline", &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}, "dial_timeout"},
		{"connection refused", &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, "connection_refused"},
		{"host unreachable", &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.EHOSTUNREACH)}, "network_unreachable"},
		{"unclassified fallback", errors.New("boom"), "upstream_error"},
	}
	for _, tc := range cases {
		if got := classifyUpstreamError(tc.err).Code; got != tc.want {
			t.Errorf("%s: classifyUpstreamError(%v).Code = %q, want %q", tc.name, tc.err, got, tc.want)
		}
	}
}

// tlsHandshakeTimeoutLike mimics net/http's unexported
// tlsHandshakeTimeoutError: a net.Error whose stable message is the only
// way to recognise it.
type tlsHandshakeTimeoutLike struct{}

func (tlsHandshakeTimeoutLike) Error() string   { return "net/http: TLS handshake timeout" }
func (tlsHandshakeTimeoutLike) Timeout() bool   { return true }
func (tlsHandshakeTimeoutLike) Temporary() bool { return true }

// selfSignedLeaf generates a self-signed certificate whose SAN list
// carries the given DNS names and IP addresses — like real internal
// appliances (e.g. a NAS with CN=root@host and no IP SAN). The
// certificate is its own trust anchor, so adding it to a RootCAs pool
// makes it trusted.
func selfSignedLeaf(t *testing.T, dnsNames []string, ips []net.IP) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	commonName := "upstream.example"
	if len(dnsNames) > 0 {
		commonName = dnsNames[0]
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		DNSNames:              dnsNames,
		IPAddresses:           ips,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, leaf
}

// startTLSServer serves handler over TLS with the given certificate and
// optional server-side TLS options (e.g. ClientAuth for mTLS upstreams).
func startTLSServer(t *testing.T, cert tls.Certificate, tlsopts func(*tls.Config)) *url.URL {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	if tlsopts != nil {
		tlsopts(cfg)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "should-not-reach")
	}), TLSConfig: cfg, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	return &url.URL{Scheme: "https", Host: ln.Addr().String()}
}

// assertClassifiedFailure drives one request through the proxy and
// asserts the classified 502: JSON body code, target in message, proxy
// error header, and the request-log row.
func assertClassifiedFailure(t *testing.T, client *http.Client, target string, sink *recordingSink, wantCode string) map[string]string {
	t.Helper()
	resp, err := client.Get(target + "/x")
	if err != nil {
		t.Fatalf("client.Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 502 (body: %s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get(brokercore.ProxyErrorHeader); got != "true" {
		t.Fatalf("missing %s header", brokercore.ProxyErrorHeader)
	}
	var errorBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errorBody); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorBody["error"] != wantCode {
		t.Fatalf("response error = %q, want %q (message: %q)", errorBody["error"], wantCode, errorBody["message"])
	}
	targetHost := target[strings.LastIndex(target, "//")+2:]
	if !strings.Contains(errorBody["message"], targetHost) {
		t.Fatalf("message %q does not mention target %q", errorBody["message"], targetHost)
	}
	rows := sink.snapshot()
	if len(rows) != 1 {
		t.Fatalf("got %d log records, want 1", len(rows))
	}
	if rows[0].ErrorCode != wantCode {
		t.Fatalf("ErrorCode = %q, want %q", rows[0].ErrorCode, wantCode)
	}
	return errorBody
}

// passthroughProxy is the shared harness for classification tests: a
// proxy whose vault passes the given host through without injection.
func passthroughProxy(t *testing.T, host string) (*url.URL, *x509.CertPool, *Proxy, *recordingSink) {
	t.Helper()
	sr := validTokenResolver("av_sess_ok",
		&brokercore.ProxyScope{VaultID: "v1", VaultName: "default", VaultRole: "proxy"})
	cp := &fakeCredProvider{byHost: map[string]fakeInjectResult{
		host: {result: &brokercore.InjectResult{Passthrough: true}},
	}}
	sink := &recordingSink{}
	proxyURL, clientRoots, p := setupProxy(t, sr, cp, func(o *Options) { o.LogSink = sink })
	return proxyURL, clientRoots, p, sink
}

// TestMITMUpstreamSelfSignedCert: the default upstream tls.Config trusts
// only system roots, so an httptest TLS upstream must surface as
// tls_unknown_authority — not a bare upstream_error.
func TestMITMUpstreamSelfSignedCert(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "should-not-reach")
	}))
	defer upstream.Close()

	upstreamHost, _, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "https://"))
	proxyURL, clientRoots, _, sink := passthroughProxy(t, upstreamHost)
	client := newTrustingClient(proxyURL, url.User("av_sess_ok"), clientRoots)

	body := assertClassifiedFailure(t, client, upstream.URL, sink, "tls_unknown_authority")
	if body["help"] == "" {
		t.Fatalf("expected non-empty help field, got %q", body["help"])
	}
}

// TestMITMUpstreamHostnameMismatch: a trusted upstream certificate that
// only carries DNS SANs fails IP-literal access — the exact shape of
// appliances like incus against https://100.x.y.z:8443.
func TestMITMUpstreamHostnameMismatch(t *testing.T) {
	cert, leaf := selfSignedLeaf(t, []string{"upstream.example"}, nil)
	upstream := startTLSServer(t, cert, nil)
	host, _, _ := net.SplitHostPort(upstream.Host)

	proxyURL, clientRoots, p, sink := passthroughProxy(t, host)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	p.upstream.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	client := newTrustingClient(proxyURL, url.User("av_sess_ok"), clientRoots)

	assertClassifiedFailure(t, client, upstream.String(), sink, "tls_hostname_mismatch")
}

// TestMITMUpstreamClientCertRequired: an mTLS upstream that demands a
// client certificate sends alert 116; the proxy must surface
// tls_client_cert_required.
func TestMITMUpstreamClientCertRequired(t *testing.T) {
	// TLS 1.3: the client aborts on hostname mismatch before the server
	// can judge the missing client certificate, so the mTLS cert must
	// also cover the dial address 127.0.0.1 for alert 116 to surface.
	cert, leaf := selfSignedLeaf(t, []string{"upstream.example"}, []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")})
	upstream := startTLSServer(t, cert, func(cfg *tls.Config) {
		cfg.ClientAuth = tls.RequireAnyClientCert
	})
	host, _, _ := net.SplitHostPort(upstream.Host)

	proxyURL, clientRoots, p, sink := passthroughProxy(t, host)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	p.upstream.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	client := newTrustingClient(proxyURL, url.User("av_sess_ok"), clientRoots)

	assertClassifiedFailure(t, client, upstream.String(), sink, "tls_client_cert_required")
}

// TestMITMUpstreamConnectionRefused: nothing listening on the target
// port surfaces as connection_refused through the CONNECT path.
func TestMITMUpstreamConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	target := "https://" + ln.Addr().String()
	host, _, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()

	proxyURL, clientRoots, _, sink := passthroughProxy(t, host)
	client := newTrustingClient(proxyURL, url.User("av_sess_ok"), clientRoots)

	assertClassifiedFailure(t, client, target, sink, "connection_refused")
}

// TestMITMUpstreamDNSFailure: a reserved-unresolvable host surfaces as
// dns_error.
func TestMITMUpstreamDNSFailure(t *testing.T) {
	proxyURL, clientRoots, _, sink := passthroughProxy(t, "nx.invalid")
	client := newTrustingClient(proxyURL, url.User("av_sess_ok"), clientRoots)

	assertClassifiedFailure(t, client, "https://nx.invalid:8443", sink, "dns_error")
}

// TestMITMForwardWebSocketConnectionRefused: the WebSocket upstream path
// shares writeUpstreamFailure, so a ws:// dial failure is classified too.
func TestMITMForwardWebSocketConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	target := "http://" + ln.Addr().String()
	host, _, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()

	proxyURL, _, _, sink := passthroughProxy(t, host)
	// Plain-http absolute-form request through the forward path, so no
	// client TLS trust is needed. Userinfo rides on the proxy URL because
	// http.Transport overwrites Proxy-Authorization from it.
	u := *proxyURL
	u.User = url.User("av_sess_ok")
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(&u)}}

	req, err := http.NewRequest("GET", target+"/ws", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	var errorBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errorBody); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorBody["error"] != "connection_refused" {
		t.Fatalf("response error = %q, want connection_refused", errorBody["error"])
	}
	rows := sink.snapshot()
	if len(rows) != 1 || rows[0].ErrorCode != "connection_refused" {
		t.Fatalf("log rows = %+v, want one connection_refused row", rows)
	}
}
