package traces

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/Infisical/agent-vault/internal/requestlog"
	"github.com/Infisical/agent-vault/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func testTracer(t *testing.T, allowlist string) (*Traces, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })
	return NewFromProvider(p, allowlist), recorder
}

func TestAllowlist(t *testing.T) {
	for _, tc := range []struct {
		value, host      string
		allowed, invalid bool
	}{
		{"", "api.example.com", false, false},
		{" API.Example.com , *.svc.example.com ", "api.EXAMPLE.com", true, false},
		{"*.svc.example.com", "a.b.svc.example.com", true, false},
		{"*.svc.example.com", "svc.example.com", false, false},
		{"*.svc.example.com", "evilsvc.example.com", false, false},
		{"api.example.com", "api.example.com.evil", false, false},
		{"api.example.com", "api.example.com.", false, false},
		{"api.example.com,*", "api.example.com", false, true},
		{"api.example.com,", "api.example.com", false, true},
		{"https://api.example.com", "api.example.com", false, true},
		{"api.example.com:443", "api.example.com", false, true},
		{"api.example.com/path", "api.example.com", false, true},
		{"10.0.0.0/8", "10.0.0.1", false, true},
		{"a..example.com", "a.example.com", false, true},
		{"-a.example.com", "a.example.com", false, true},
		{"foo.*.example.com", "foo.api.example.com", false, true},
	} {
		t.Run(tc.value+"/"+tc.host, func(t *testing.T) {
			patterns, err := parseAllowlist(tc.value)
			if (err != nil) != tc.invalid {
				t.Fatalf("parse error = %v", err)
			}
			tr := &Traces{allowlist: patterns}
			if got := tr.allows(tc.host); got != tc.allowed {
				t.Fatalf("allowed = %v", got)
			}
		})
	}
}

func TestPropagationAndSampling(t *testing.T) {
	for _, sampled := range []bool{true, false} {
		for _, allowed := range []bool{true, false} {
			t.Run(fmt.Sprintf("sampled=%v/allowed=%v", sampled, allowed), func(t *testing.T) {
				patterns := ""
				if allowed {
					patterns = "api.example.com"
				}
				tr, recorder := testTracer(t, patterns)
				parentProvider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
				defer func() { _ = parentProvider.Shutdown(context.Background()) }()
				_, parent := parentProvider.Tracer("caller").Start(context.Background(), "caller")
				defer parent.End()
				sc := parent.SpanContext()
				if !sampled {
					sc = sc.WithTraceFlags(0)
				}
				state, err := trace.ParseTraceState("vendor=opaque")
				if err != nil {
					t.Fatal(err)
				}
				sc = sc.WithTraceState(state)
				incoming := http.Header{"Baggage": {"tenant=example"}}
				propagation.TraceContext{}.Inject(trace.ContextWithSpanContext(context.Background(), sc), propagation.HeaderCarrier(incoming))
				ctx := tr.Extract(context.Background(), incoming)
				ctx, span := tr.Start(ctx, "request", trace.SpanKindServer)
				outgoing := incoming.Clone()
				outgoing["traceparent"] = []string{"stale"}
				tr.Inject(ctx, "api.example.com", outgoing)
				if allowed {
					outCtx := propagation.TraceContext{}.Extract(context.Background(), propagation.HeaderCarrier(outgoing))
					got := trace.SpanContextFromContext(outCtx)
					current := trace.SpanContextFromContext(ctx)
					if got.TraceID() != sc.TraceID() || got.SpanID() != current.SpanID() || got.IsSampled() != sampled {
						t.Fatalf("incorrect propagated context: %v", got)
					}
					if outgoing.Get("Baggage") != incoming.Get("Baggage") || outgoing.Get("Tracestate") != "vendor=opaque" {
						t.Fatal("allowed context not preserved")
					}
				} else if len(outgoing) != 0 {
					t.Fatalf("context leaked: %v", outgoing)
				}
				span.End("")
				if got := len(recorder.Ended()); got != boolInt(sampled) {
					t.Fatalf("ended = %d", got)
				}
				if sampled && recorder.Ended()[0].Parent().SpanID() != sc.SpanID() {
					t.Fatal("remote parent not preserved")
				}
			})
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestNilAndMalformedContext(t *testing.T) {
	var disabled *Traces
	headers := http.Header{"Traceparent": {"invalid"}, "Tracestate": {"vendor=old"}, "Baggage": {"opaque"}}
	before := headers.Clone()
	ctx := context.Background()
	disabled.Inject(ctx, "external.example", headers)
	if !reflect.DeepEqual(before, headers) || disabled.Extract(ctx, headers) != ctx {
		t.Fatal("disabled tracing changed forwarding")
	}
	_, span := disabled.Start(ctx, "noop", trace.SpanKindClient)
	span.End("")
	span.ResponseHeaders(500)
	span.EndRequest(requestlog.Record{})
	if err := disabled.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	tr, recorder := testTracer(t, "api.example.com")
	ctx, span = tr.Start(tr.Extract(ctx, headers), "root", trace.SpanKindServer)
	tr.Inject(ctx, "api.example.com", headers)
	if headers.Get("Tracestate") != "" || headers.Get("Traceparent") == "invalid" {
		t.Fatal("stale trace context survived injection")
	}
	span.End("")
	if len(recorder.Ended()) != 1 || recorder.Ended()[0].Parent().IsValid() {
		t.Fatal("malformed context did not create a root")
	}
}

func TestOutcomeAndEndOnce(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		want   codes.Code
	}{
		{200, "", codes.Unset}, {403, "no_match", codes.Unset}, {429, "rate_limited", codes.Unset},
		{401, "", codes.Unset}, {503, "", codes.Error}, {200, "response_transfer_error", codes.Error},
	} {
		tr, recorder := testTracer(t, "")
		_, span := tr.Start(context.Background(), "request", trace.SpanKindServer)
		span.EndRequest(requestlog.Record{Status: tc.status, ErrorCode: tc.code})
		span.End("internal")
		ended := recorder.Ended()
		if len(ended) != 1 || ended[0].Status().Code != tc.want {
			t.Fatalf("%d/%s: wrong outcome", tc.status, tc.code)
		}
	}
}

func scrubEnv(t *testing.T) {
	t.Helper()
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
}

func TestEnvironmentGating(t *testing.T) {
	scrubEnv(t)
	tr, err := NewFromEnv("test", nil)
	if err != nil || tr != nil {
		t.Fatalf("no endpoint: %v, %v", tr, err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "invalid")
	if tr, err = NewFromEnv("test", nil); err == nil || tr != nil {
		t.Fatal("invalid protocol should disable tracing")
	}
	t.Setenv("OTEL_SDK_DISABLED", "true")
	if tr, err = NewFromEnv("test", nil); err != nil || tr != nil {
		t.Fatal("SDK disable ignored")
	}
}

type traceCollector struct {
	collector.UnimplementedTraceServiceServer
	requests chan *collector.ExportTraceServiceRequest
}

func (c *traceCollector) Export(_ context.Context, r *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	c.requests <- r
	return &collector.ExportTraceServiceResponse{}, nil
}

func TestOTLPExportAndSamplerEnvironment(t *testing.T) {
	for _, protocol := range []string{"grpc", "http/protobuf"} {
		t.Run(protocol, func(t *testing.T) {
			scrubEnv(t)
			requests := make(chan *collector.ExportTraceServiceRequest, 1)
			var endpoint string
			if protocol == "grpc" {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				server := grpc.NewServer()
				collector.RegisterTraceServiceServer(server, &traceCollector{requests: requests})
				go func() { _ = server.Serve(listener) }()
				t.Cleanup(server.Stop)
				endpoint = "http://" + listener.Addr().String()
			} else {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1/traces" {
						t.Errorf("path = %s", r.URL.Path)
					}
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					var req collector.ExportTraceServiceRequest
					if err := proto.Unmarshal(data, &req); err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					requests <- &req
					w.Header().Set("Content-Type", "application/x-protobuf")
				}))
				t.Cleanup(server.Close)
				endpoint = server.URL + "/v1/traces"
			}
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", protocol)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "invalid") // per-signal wins
			t.Setenv("OTEL_SERVICE_NAME", "traces-test")
			t.Setenv(AllowlistEnv, "invalid:443")
			tr, err := NewFromEnv("test-version", nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, span := tr.Start(context.Background(), "exported", trace.SpanKindServer)
			if !trace.SpanFromContext(ctx).IsRecording() {
				t.Fatal("root should sample by default")
			}
			span.End("")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tr.Shutdown(shutdownCtx); err != nil {
				t.Fatal(err)
			}
			select {
			case req := <-requests:
				if len(req.ResourceSpans) != 1 || len(req.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
					t.Fatal("unexpected exported batch")
				}
				attrs := req.ResourceSpans[0].Resource.Attributes
				found := false
				for _, attr := range attrs {
					if attr.Key == "service.name" && attr.Value.GetStringValue() == "traces-test" {
						found = true
					}
				}
				if !found {
					t.Fatal("resource environment override missing")
				}
			case <-time.After(time.Second):
				t.Fatal("no exported spans")
			}
			t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0")
			tr, err = NewFromEnv("test", nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, span = tr.Start(context.Background(), "dropped", trace.SpanKindServer)
			if trace.SpanFromContext(ctx).IsRecording() {
				t.Fatal("sampler environment ignored")
			}
			span.End("")
			if err := tr.Shutdown(shutdownCtx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type failingExporter struct{ err error }

func (e failingExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return e.err }
func (failingExporter) Shutdown(context.Context) error                               { return nil }

func TestExporterFailures(t *testing.T) {
	want := errors.New("collector unavailable")
	count := 0
	exp := &countingExporter{SpanExporter: failingExporter{want}, onFailure: func() { count++ }}
	for range 2 {
		if err := exp.ExportSpans(context.Background(), nil); !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
	if count != 2 || exp.lastWarn.IsZero() {
		t.Fatal("missing failure observability")
	}
	exp.SpanExporter = failingExporter{}
	if err := exp.ExportSpans(context.Background(), nil); err != nil || count != 2 {
		t.Fatal("success counted as failure")
	}
}
