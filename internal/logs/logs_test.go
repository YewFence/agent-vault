package logs

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/Infisical/agent-vault/internal/requestlog"
)

// compile-time guarantee that *Logs satisfies the sink seam.
var _ requestlog.Sink = (*Logs)(nil)

type captureProcessor struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (p *captureProcessor) OnEmit(_ context.Context, r *sdklog.Record) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, r.Clone())
	return nil
}
func (p *captureProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }
func (p *captureProcessor) Shutdown(context.Context) error                         { return nil }
func (p *captureProcessor) ForceFlush(context.Context) error                       { return nil }

func (p *captureProcessor) only(t *testing.T) sdklog.Record {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.records) != 1 {
		t.Fatalf("captured records = %d, want 1", len(p.records))
	}
	return p.records[0]
}

func testLogs(t *testing.T) (*Logs, *captureProcessor) {
	t.Helper()
	proc := &captureProcessor{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc))
	l := NewFromProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return l, proc
}

func TestRecordTraceCorrelation(t *testing.T) {
	l, proc := testLogs(t)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	defer span.End()
	l.Record(ctx, sampleRecord())
	rec := proc.only(t)
	if rec.TraceID() != span.SpanContext().TraceID() || rec.SpanID() != span.SpanContext().SpanID() {
		t.Fatal("OTLP log is not correlated with the active request span")
	}
}

func attrMap(r sdklog.Record) map[string]string {
	out := map[string]string{}
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		out[string(kv.Key)] = kv.Value.String()
		return true
	})
	return out
}

func sampleRecord() requestlog.Record {
	return requestlog.Record{
		VaultID:        "vlt_1",
		ActorType:      "agent",
		ActorID:        "agt_1",
		Ingress:        "mitm",
		Method:         "POST",
		Host:           "api.github.com",
		Path:           "/repos/o/r/issues",
		MatchedService: "github",
		MatchedHost:    "*.github.com",
		CredentialKeys: []string{"GITHUB_TOKEN"},
		Status:         201,
		LatencyMs:      142,
		AuthScheme:     "bearer",
		AuthHeader:     "Authorization",
	}
}

func TestRecordSuccess(t *testing.T) {
	l, proc := testLogs(t)
	l.Record(context.Background(), sampleRecord())

	rec := proc.only(t)
	if rec.Severity() != otellog.SeverityInfo {
		t.Errorf("severity = %v, want INFO", rec.Severity())
	}
	if rec.EventName() != eventName {
		t.Errorf("event name = %q, want %q", rec.EventName(), eventName)
	}
	if body := rec.Body().AsString(); body != "POST api.github.com/repos/o/r/issues → 201 (142ms)" {
		t.Errorf("body = %q", body)
	}
	if rec.Timestamp().IsZero() {
		t.Error("timestamp is zero")
	}
	attrs := attrMap(rec)
	want := map[string]string{
		"http.request.method":         "POST",
		"server.address":              "api.github.com",
		"url.path":                    "/repos/o/r/issues",
		"http.response.status_code":   "201",
		"agent_vault.ingress":         "mitm",
		"agent_vault.vault_id":        "vlt_1",
		"agent_vault.actor_type":      "agent",
		"agent_vault.actor_id":        "agt_1",
		"agent_vault.matched_service": "github",
		"agent_vault.matched_host":    "*.github.com",
		"agent_vault.auth_scheme":     "bearer",
		"agent_vault.auth_header":     "Authorization",
		"agent_vault.latency_ms":      "142",
	}
	for k, v := range want {
		if attrs[k] != v {
			t.Errorf("attribute %q = %q, want %q", k, attrs[k], v)
		}
	}
	var keys []string
	rec.WalkAttributes(func(kv attribute.KeyValue) bool {
		if string(kv.Key) == "agent_vault.credential_keys" {
			keys = kv.Value.AsStringSlice()
		}
		return true
	})
	if len(keys) != 1 || keys[0] != "GITHUB_TOKEN" {
		t.Errorf("credential_keys = %v, want [GITHUB_TOKEN]", keys)
	}
	if _, ok := attrs["agent_vault.error_code"]; ok {
		t.Error("error_code present on a successful request")
	}
}

func TestRecordSeverity(t *testing.T) {
	l, proc := testLogs(t)

	denial := sampleRecord()
	denial.Status = 401
	denial.ErrorCode = "auth_failed"
	l.Record(context.Background(), denial)

	upstream := sampleRecord()
	upstream.Status = 502
	upstream.ErrorCode = "upstream_error"
	l.Record(context.Background(), upstream)

	proc.mu.Lock()
	defer proc.mu.Unlock()
	if len(proc.records) != 2 {
		t.Fatalf("captured records = %d, want 2", len(proc.records))
	}
	if got := proc.records[0].Severity(); got != otellog.SeverityWarn {
		t.Errorf("denial severity = %v, want WARN", got)
	}
	if got := attrMap(proc.records[0])["agent_vault.error_code"]; got != "auth_failed" {
		t.Errorf("denial error_code = %q", got)
	}
	if got := proc.records[1].Severity(); got != otellog.SeverityError {
		t.Errorf("upstream failure severity = %v, want ERROR", got)
	}
}

func TestRecordUnmatchedService(t *testing.T) {
	l, proc := testLogs(t)
	r := sampleRecord()
	r.MatchedService = ""
	l.Record(context.Background(), r)
	if got := attrMap(proc.only(t))["agent_vault.matched_service"]; got != "unmatched" {
		t.Errorf("matched_service = %q, want unmatched", got)
	}
}

func TestNilLogsSafe(t *testing.T) {
	var l *Logs
	l.Record(context.Background(), sampleRecord())
	if err := l.Shutdown(context.Background()); err != nil {
		t.Fatalf("nil Shutdown: %v", err)
	}
}

type failingExporter struct{}

func (f failingExporter) Export(context.Context, []sdklog.Record) error {
	return errors.New("export failed")
}
func (failingExporter) Shutdown(context.Context) error   { return nil }
func (failingExporter) ForceFlush(context.Context) error { return nil }

func TestCountingExporterCallsFailure(t *testing.T) {
	called := 0
	e := &countingExporter{Exporter: failingExporter{}, onFailure: func() { called++ }}
	if err := e.Export(context.Background(), nil); err == nil {
		t.Fatal("Export error = nil, want error")
	}
	if called != 1 {
		t.Fatalf("failure callback count = %d, want 1", called)
	}
}

func scrubEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"OTEL_SDK_DISABLED",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
	} {
		t.Setenv(v, "")
	}
}

func TestNewFromEnvGating(t *testing.T) {
	scrubEnv(t)
	if l, err := NewFromEnv("test", nil); err != nil || l != nil {
		t.Fatalf("no endpoint = %v, %v; want nil, nil", l, err)
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4317")
	t.Setenv("OTEL_SDK_DISABLED", "true")
	if l, err := NewFromEnv("test", nil); err != nil || l != nil {
		t.Fatalf("SDK disabled = %v, %v; want nil, nil", l, err)
	}

	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "json")
	if _, err := NewFromEnv("test", nil); err == nil {
		t.Fatal("unsupported protocol: err = nil")
	}
}

// TestNewFromEnvEnabled covers the enabled path with the lazy HTTP
// transport (grpc New is covered implicitly in production; a real dial
// has no place in a unit test).
func TestNewFromEnvEnabled(t *testing.T) {
	scrubEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http/protobuf")
	l, err := NewFromEnv("test", nil)
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}
	if l == nil {
		t.Fatal("logs endpoint set: Logs = nil, want enabled")
	}
	if err := l.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
