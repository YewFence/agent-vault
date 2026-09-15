package mitm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/Infisical/agent-vault/internal/brokercore"
	"github.com/Infisical/agent-vault/internal/metrics"
	"github.com/Infisical/agent-vault/internal/requestlog"
	"github.com/Infisical/agent-vault/internal/traces"
)

type traceLogSink struct {
	context trace.SpanContext
	records []requestlog.Record
}

func (s *traceLogSink) Record(ctx context.Context, r requestlog.Record) {
	s.context = trace.SpanContextFromContext(ctx)
	s.records = append(s.records, r)
}

func proxyTracer(t *testing.T, allowlist string) (*traces.Traces, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return traces.NewFromProvider(provider, allowlist), recorder
}

func TestProxyTraces(t *testing.T) {
	for _, tc := range []struct {
		name                                                    string
		allowed, disabled, denial, retry, retryFailure, noRetry bool
		upstreamStatus                                          int
	}{
		{name: "blocked propagation", upstreamStatus: 200},
		{name: "allowed propagation", allowed: true, upstreamStatus: 200},
		{name: "disabled preserves headers", disabled: true, upstreamStatus: 200},
		{name: "denial", denial: true},
		{name: "upstream 500", upstreamStatus: 500},
		{name: "retry", allowed: true, retry: true, upstreamStatus: 200},
		{name: "retry failure", retry: true, retryFailure: true},
		{name: "401 without retry headers", noRetry: true, upstreamStatus: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENT_VAULT_ALLOW_PRIVATE_RANGES", "true")
			received := make(chan http.Header, 2)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- r.Header.Clone()
				if r.URL.Path != "/credential/real-secret" {
					t.Errorf("rewritten path = %s", r.URL.Path)
				}
				if tc.retry && len(received) == 1 {
					w.WriteHeader(401)
					return
				}
				if tc.retryFailure {
					panic(http.ErrAbortHandler)
				}
				w.WriteHeader(tc.upstreamStatus)
				_, _ = io.WriteString(w, "response")
			}))
			defer upstream.Close()
			u, err := url.Parse(upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			host := u.Hostname()
			port, _ := strconv.Atoi(u.Port())
			allowlist := ""
			if tc.allowed {
				allowlist = host
			}
			tr, recorder := proxyTracer(t, allowlist)
			if tc.disabled {
				tr = nil
			}
			inject := &brokercore.InjectResult{
				MatchedName: "upstream", Headers: map[string]string{"Authorization": "Bearer real-secret"},
				Substitutions: []brokercore.ResolvedSubstitution{{Placeholder: "placeholder", Value: "real-secret", In: []string{"path"}}},
			}
			if tc.noRetry {
				inject.Headers = nil
			}
			cp := &fakeCredProvider{byHost: map[string]fakeInjectResult{host: {result: inject}}}
			if tc.denial {
				cp.byHost = nil
			}
			sink := &traceLogSink{}
			reader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithExemplarFilter(exemplar.TraceBasedFilter))
			defer func() { _ = meterProvider.Shutdown(context.Background()) }()
			m, err := metrics.NewFromProvider(meterProvider)
			if err != nil {
				t.Fatal(err)
			}
			p := New("", Options{Credentials: cp, Traces: tr, Metrics: m, LogSink: sink, Logger: slog.New(slog.DiscardHandler)})
			p.upstream.DisableKeepAlives = true
			defer p.upstream.CloseIdleConnections()
			r := httptest.NewRequest(http.MethodGet, upstream.URL+"/credential/placeholder?secret=query", nil)
			r.Host = "spoofed.internal.example"
			r.Header.Set("Traceparent", "invalid-client-context")
			r.Header.Set("Tracestate", "vendor=stale")
			r.Header.Set("Baggage", "tenant=opaque")
			w := httptest.NewRecorder()
			p.forwardRequest(w, r, u.Host, host, port, false, &brokercore.ProxyScope{VaultID: "vault", AgentID: "agent"})
			if len(sink.records) != 1 {
				t.Fatalf("log count = %d", len(sink.records))
			}
			ended := recorder.Ended()
			if tc.disabled {
				if len(ended) != 0 {
					t.Fatal("disabled tracing emitted spans")
				}
				h := <-received
				if h.Get("Traceparent") != "invalid-client-context" || h.Get("Baggage") != "tenant=opaque" {
					t.Fatal("disabled tracing changed headers")
				}
				return
			}
			var root sdktrace.ReadOnlySpan
			var attempts []sdktrace.ReadOnlySpan
			for _, span := range ended {
				if strings.Contains(fmt.Sprint(span.Attributes()), "real-secret") || strings.Contains(fmt.Sprint(span.Attributes()), "query") {
					t.Fatal("sensitive URL exported")
				}
				switch span.Name() {
				case "agent_vault.proxy.request":
					root = span
				case "agent_vault.proxy.upstream":
					attempts = append(attempts, span)
				}
			}
			if root == nil || root.SpanContext().SpanID() != sink.context.SpanID() || root.SpanContext().TraceID() != sink.context.TraceID() {
				t.Fatal("request log context not correlated")
			}
			assertDurationExemplar(t, reader, root.SpanContext())
			for _, span := range ended {
				if span != root && span.Parent().SpanID() != root.SpanContext().SpanID() {
					t.Fatalf("wrong parent for %s", span.Name())
				}
			}
			if tc.denial {
				if len(attempts) != 0 || root.Status().Code != codes.Unset || sink.records[0].ErrorCode != "no_match" {
					t.Fatal("denial outcome incorrect")
				}
				return
			}
			wantAttempts := 1
			if tc.retry {
				wantAttempts = 2
			}
			if len(attempts) != wantAttempts {
				t.Fatalf("attempts = %d, want %d", len(attempts), wantAttempts)
			}
			for i, attempt := range attempts {
				h := <-received
				if tc.allowed {
					ctx := propagation.TraceContext{}.Extract(context.Background(), propagation.HeaderCarrier(h))
					sc := trace.SpanContextFromContext(ctx)
					if sc.SpanID() != attempt.SpanContext().SpanID() || sc.TraceID() != root.SpanContext().TraceID() {
						t.Fatalf("attempt %d propagation mismatch", i)
					}
					if h.Get("Baggage") != "tenant=opaque" {
						t.Fatal("allowed baggage lost")
					}
				} else if h.Get("Traceparent") != "" || h.Get("Tracestate") != "" || h.Get("Baggage") != "" {
					t.Fatal("blocked context leaked")
				}
				if !tc.retryFailure || i == 0 {
					if len(attempt.Events()) != 1 || attempt.Events()[0].Name != "http.response.headers" {
						t.Fatal("missing headers event")
					}
				}
			}
			wantRoot := codes.Unset
			if tc.retryFailure || tc.upstreamStatus >= 500 {
				wantRoot = codes.Error
			}
			if root.Status().Code != wantRoot {
				t.Fatalf("root status = %v", root.Status())
			}
			if tc.retry && attempts[0].Status().Code != codes.Error {
				t.Fatal("failed first attempt not marked")
			}
			if tc.retryFailure && (w.Code != 502 || sink.records[0].ErrorCode != "upstream_error") {
				t.Fatal("failed retry not reported")
			}
			if tc.noRetry && w.Body.String() != "response" {
				t.Fatal("first response closed without a retry")
			}
		})
	}
}

func assertDurationExemplar(t *testing.T, reader *sdkmetric.ManualReader, sc trace.SpanContext) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "agent_vault.proxy.duration" {
				continue
			}
			histogram := metric.Data.(metricdata.Histogram[float64])
			for _, point := range histogram.DataPoints {
				for _, ex := range point.Exemplars {
					traceID, spanID := sc.TraceID(), sc.SpanID()
					if bytes.Equal(ex.TraceID, traceID[:]) && bytes.Equal(ex.SpanID, spanID[:]) {
						return
					}
				}
			}
		}
	}
	t.Fatal("duration metric lacks a request-span exemplar")
}

func TestTraceSpansWaitForResponseBody(t *testing.T) {
	t.Setenv("AGENT_VAULT_ALLOW_PRIVATE_RANGES", "true")
	release := make(chan struct{})
	ready := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		close(ready)
		<-release
		_, _ = io.WriteString(w, "data: last\n\n")
	}))
	defer upstream.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())
	tr, recorder := proxyTracer(t, "")
	p := New("", Options{Traces: tr, Logger: slog.New(slog.DiscardHandler), Credentials: &fakeCredProvider{byHost: map[string]fakeInjectResult{
		u.Hostname(): {result: &brokercore.InjectResult{Passthrough: true}},
	}}})
	defer p.upstream.CloseIdleConnections()
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.forwardRequest(httptest.NewRecorder(), httptest.NewRequest("GET", upstream.URL, nil), u.Host, u.Hostname(), port, false, &brokercore.ProxyScope{})
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not start")
	}
	for _, span := range recorder.Ended() {
		if span.Name() == "agent_vault.proxy.request" || span.Name() == "agent_vault.proxy.upstream" {
			t.Fatal("span ended before response body")
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not finish")
	}
	if len(recorder.Ended()) != 4 {
		t.Fatalf("ended = %d", len(recorder.Ended()))
	}
}

func TestTraceResponseTransferFailure(t *testing.T) {
	t.Setenv("AGENT_VAULT_ALLOW_PRIVATE_RANGES", "true")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "short")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())
	tr, recorder := proxyTracer(t, "")
	sink := &traceLogSink{}
	p := New("", Options{Traces: tr, LogSink: sink, Logger: slog.New(slog.DiscardHandler), Credentials: &fakeCredProvider{byHost: map[string]fakeInjectResult{
		u.Hostname(): {result: &brokercore.InjectResult{Passthrough: true}},
	}}})
	defer p.upstream.CloseIdleConnections()
	p.forwardRequest(httptest.NewRecorder(), httptest.NewRequest("GET", upstream.URL, nil), u.Host, u.Hostname(), port, false, &brokercore.ProxyScope{})
	if len(sink.records) != 1 || sink.records[0].ErrorCode != "response_transfer_error" {
		t.Fatal("transfer failure not logged")
	}
	for _, span := range recorder.Ended() {
		if span.Name() == "agent_vault.proxy.request" || span.Name() == "agent_vault.proxy.upstream" {
			if span.Status().Code != codes.Error {
				t.Fatal("transfer failure not traced")
			}
		}
	}
}
