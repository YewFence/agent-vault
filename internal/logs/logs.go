// Package logs exports Agent Vault's per-request proxy records as OTel
// log records over OTLP. Each record mirrors requestlog.Record — already
// secret-free by contract (no bodies, no header values, no query
// strings) — with semconv HTTP attributes where defined and agent_vault.*
// names for broker concepts. The exporter setup shares endpoint gating,
// protocol selection, and resource identity with metrics via
// internal/otlp.
package logs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/Infisical/agent-vault/internal/brokercore"
	"github.com/Infisical/agent-vault/internal/otlp"
	"github.com/Infisical/agent-vault/internal/requestlog"
)

const loggerName = "agent_vault.proxy"

// eventName marks every emitted record so backends can select Agent Vault
// proxy traffic without parsing bodies.
const eventName = "agent_vault.proxy.request"

// Logs emits one OTel log record per proxied request. A nil *Logs is a
// valid no-op sink, matching the nil-safe pattern of metrics.Metrics.
type Logs struct {
	provider *sdklog.LoggerProvider
	logger   otellog.Logger
}

// NewFromProvider builds the emitter on an already configured provider.
// Tests use this with a capturing processor; production uses NewFromEnv.
func NewFromProvider(provider *sdklog.LoggerProvider) *Logs {
	if provider == nil {
		return nil
	}
	return &Logs{provider: provider, logger: provider.Logger(loggerName)}
}

// NewFromEnv creates an OTLP logs provider from the shared OTEL_*
// configuration. No endpoint (generic or logs-specific) means disabled.
// onExportFailure runs on every failed export — typically counting via
// metrics.RecordExporterFailure — and failures additionally surface in
// local logs through a throttled slog warning, since a log exporter
// cannot report its own death through itself.
func NewFromEnv(version string, onExportFailure func()) (*Logs, error) {
	if !otlp.Enabled("logs") {
		return nil, nil
	}
	ctx := context.Background()
	protocol, err := otlp.Protocol("logs")
	if err != nil {
		return nil, err
	}
	var exp sdklog.Exporter
	switch protocol {
	case otlp.ProtocolGRPC:
		exp, err = otlploggrpc.New(ctx)
	case otlp.ProtocolHTTPProtobuf:
		exp, err = otlploghttp.New(ctx)
	}
	if err != nil {
		return nil, err
	}
	res, err := otlp.NewResource(ctx, version)
	if err != nil {
		return nil, err
	}
	counted := &countingExporter{Exporter: exp, onFailure: onExportFailure}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(counted)),
		sdklog.WithResource(res),
	)
	return NewFromProvider(provider), nil
}

// countingExporter wraps the real exporter to observe failures: every
// failure runs onFailure, and a throttled slog warning keeps a dead
// collector visible locally without one line per batch attempt.
type countingExporter struct {
	sdklog.Exporter
	onFailure func()
	mu        sync.Mutex
	lastWarn  time.Time
}

func (e *countingExporter) Export(ctx context.Context, records []sdklog.Record) error {
	err := e.Exporter.Export(ctx, records)
	if err == nil {
		return nil
	}
	if e.onFailure != nil {
		e.onFailure()
	}
	e.mu.Lock()
	throttled := time.Since(e.lastWarn) < time.Minute
	if !throttled {
		e.lastWarn = time.Now()
	}
	e.mu.Unlock()
	if !throttled {
		slog.Warn("otlp logs export failed", //nolint:gosec // G706: structured slog attrs, handlers quote control chars
			slog.String("err", err.Error()))
	}
	return err
}

// Record implements requestlog.Sink. Emit only enqueues onto the batch
// processor, so the proxy hot path is never blocked by export I/O.
func (l *Logs) Record(ctx context.Context, r requestlog.Record) {
	if l == nil {
		return
	}
	var rec otellog.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverity(severity(r))
	rec.SetEventName(eventName)
	rec.SetBody(attribute.StringValue(body(r)))
	rec.AddAttributes(attrs(r)...)
	l.logger.Emit(ctx, rec)
}

// Shutdown flushes pending records and stops the provider.
func (l *Logs) Shutdown(ctx context.Context) error {
	if l == nil || l.provider == nil {
		return nil
	}
	return l.provider.Shutdown(ctx)
}

// severity maps outcome to level: successful forwards are INFO, requests
// Agent Vault refused are WARN (shared brokercore.IsDenial taxonomy), and
// proxy or upstream failures are ERROR.
func severity(r requestlog.Record) otellog.Severity {
	switch {
	case r.ErrorCode == "":
		return otellog.SeverityInfo
	case brokercore.IsDenial(r.ErrorCode):
		return otellog.SeverityWarn
	default:
		return otellog.SeverityError
	}
}

// body renders a one-line human summary; the structured fields always
// live in attributes, the body only serves log UIs.
func body(r requestlog.Record) string {
	target := r.Host + r.Path
	if r.ErrorCode != "" {
		return fmt.Sprintf("%s %s → %s (%dms)", r.Method, target, r.ErrorCode, r.LatencyMs)
	}
	return fmt.Sprintf("%s %s → %d (%dms)", r.Method, target, r.Status, r.LatencyMs)
}

// attrs mirrors the full requestlog.Record: logs are not an aggregated
// signal, so identity and target fields that metrics must keep
// low-cardinality are welcome here.
func attrs(r requestlog.Record) []attribute.KeyValue {
	kv := []attribute.KeyValue{
		attribute.String("http.request.method", r.Method),
		attribute.String("server.address", r.Host),
		attribute.String("url.path", r.Path),
		attribute.Int("http.response.status_code", r.Status),
		attribute.String("agent_vault.ingress", r.Ingress),
		attribute.String("agent_vault.vault_id", r.VaultID),
		attribute.String("agent_vault.actor_type", r.ActorType),
		attribute.String("agent_vault.actor_id", r.ActorID),
		attribute.String("agent_vault.matched_service", matchedService(r)),
		attribute.String("agent_vault.matched_host", r.MatchedHost),
		attribute.String("agent_vault.auth_scheme", r.AuthScheme),
		attribute.String("agent_vault.auth_header", r.AuthHeader),
		attribute.Int64("agent_vault.latency_ms", r.LatencyMs),
	}
	if r.ErrorCode != "" {
		kv = append(kv, attribute.String("agent_vault.error_code", r.ErrorCode))
	}
	if r.MatchedPath != "" {
		kv = append(kv, attribute.String("agent_vault.matched_path", r.MatchedPath))
	}
	if r.MatchedPort != nil {
		kv = append(kv, attribute.Int("agent_vault.matched_port", *r.MatchedPort))
	}
	if len(r.CredentialKeys) > 0 {
		kv = append(kv, attribute.StringSlice("agent_vault.credential_keys", r.CredentialKeys))
	}
	return kv
}

// matchedService keeps the metrics convention of an explicit "unmatched"
// bucket instead of an empty string.
func matchedService(r requestlog.Record) string {
	if r.MatchedService == "" {
		return "unmatched"
	}
	return r.MatchedService
}
