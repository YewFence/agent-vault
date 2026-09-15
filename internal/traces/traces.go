// Package traces owns proxy spans and opt-in outbound W3C propagation.
// Only pre-substitution metadata belongs here: outbound URLs and raw errors
// can contain resolved credentials and must never become span attributes.
package traces

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/Infisical/agent-vault/internal/brokercore"
	"github.com/Infisical/agent-vault/internal/otlp"
	"github.com/Infisical/agent-vault/internal/requestlog"
)

const AllowlistEnv = "AGENT_VAULT_TRACE_PROPAGATION_ALLOWLIST"

type Traces struct {
	provider  *sdktrace.TracerProvider
	tracer    trace.Tracer
	allowlist []string
}

// NewFromProvider is the injection seam for tests. Invalid allowlists fail
// closed without disabling spans, just as in production.
func NewFromProvider(provider *sdktrace.TracerProvider, allowlist string) *Traces {
	if provider == nil {
		return nil
	}
	patterns, err := parseAllowlist(allowlist)
	if err != nil {
		slog.Warn("trace propagation allowlist invalid; outbound context disabled")
	}
	return &Traces{provider: provider, tracer: provider.Tracer("agent_vault.proxy"), allowlist: patterns}
}

func NewFromEnv(version string, onExportFailure func()) (*Traces, error) {
	if !otlp.Enabled("traces") {
		return nil, nil
	}
	protocol, err := otlp.Protocol("traces")
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	res, err := otlp.NewResource(ctx, version)
	if err != nil {
		return nil, err
	}
	var exp sdktrace.SpanExporter
	switch protocol {
	case otlp.ProtocolGRPC:
		exp, err = otlptracegrpc.New(ctx)
	case otlp.ProtocolHTTPProtobuf:
		exp, err = otlptracehttp.New(ctx)
	}
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(&countingExporter{SpanExporter: exp, onFailure: onExportFailure}),
	)
	// The SDK reads OTEL_TRACES_SAMPLER/ARG; an absent or unknown sampler
	// defaults to ParentBased(AlwaysSample). The batch queue is non-blocking.
	return NewFromProvider(provider, os.Getenv(AllowlistEnv)), nil
}

func (t *Traces) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

type countingExporter struct {
	sdktrace.SpanExporter
	onFailure func()
	mu        sync.Mutex
	lastWarn  time.Time
}

func (e *countingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	if err == nil {
		return nil
	}
	if e.onFailure != nil {
		e.onFailure()
	}
	e.mu.Lock()
	warn := time.Since(e.lastWarn) >= time.Minute
	if warn {
		e.lastWarn = time.Now()
	}
	e.mu.Unlock()
	if warn {
		slog.Warn("otlp traces export failed", "err", err.Error()) //nolint:gosec // G706: structured slog attributes
	}
	return err
}

// Span deliberately accepts only safe machine-readable error codes.
type Span struct {
	span trace.Span
	once sync.Once
}

func (t *Traces) Start(ctx context.Context, name string, kind trace.SpanKind, attrs ...attribute.KeyValue) (context.Context, *Span) {
	if t == nil {
		return ctx, nil
	}
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(kind), trace.WithAttributes(attrs...))
	return ctx, &Span{span: span}
}

func (s *Span) End(code string) {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if code != "" {
			s.span.SetAttributes(attribute.String("agent_vault.error_code", code))
			if !brokercore.IsDenial(code) {
				s.span.SetStatus(codes.Error, code)
			}
		}
		s.span.End()
	})
}

func (s *Span) ResponseHeaders(status int) {
	if s == nil {
		return
	}
	s.span.AddEvent("http.response.headers")
	s.span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status >= 400 {
		s.span.SetStatus(codes.Error, "upstream_http_error")
	}
}

func (s *Span) EndRequest(r requestlog.Record) {
	if s == nil {
		return
	}
	s.span.SetAttributes(recordAttrs(r)...)
	if r.Status >= 500 {
		s.span.SetStatus(codes.Error, "http_server_error")
	}
	s.End(r.ErrorCode)
}

// TargetAttrs always describes the incoming, not credential-rewritten, URL.
func TargetAttrs(method, host, path string, port int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("http.request.method", method),
		attribute.String("server.address", host),
		attribute.Int("server.port", port),
		attribute.String("url.path", path),
	}
}

func recordAttrs(r requestlog.Record) []attribute.KeyValue {
	service := r.MatchedService
	if service == "" {
		service = "unmatched"
	}
	a := []attribute.KeyValue{
		attribute.Int("http.response.status_code", r.Status),
		attribute.String("agent_vault.ingress", r.Ingress),
		attribute.String("agent_vault.vault_id", r.VaultID),
		attribute.String("agent_vault.actor_type", r.ActorType),
		attribute.String("agent_vault.actor_id", r.ActorID),
		attribute.String("agent_vault.matched_service", service),
		attribute.String("agent_vault.matched_host", r.MatchedHost),
		attribute.String("agent_vault.auth_scheme", r.AuthScheme),
		attribute.String("agent_vault.auth_header", r.AuthHeader),
		attribute.Int64("agent_vault.latency_ms", r.LatencyMs),
	}
	if r.MatchedPath != "" {
		a = append(a, attribute.String("agent_vault.matched_path", r.MatchedPath))
	}
	if r.MatchedPort != nil {
		a = append(a, attribute.Int("agent_vault.matched_port", *r.MatchedPort))
	}
	if len(r.CredentialKeys) > 0 {
		a = append(a, attribute.StringSlice("agent_vault.credential_keys", r.CredentialKeys))
	}
	return a
}
