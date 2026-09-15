package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/Infisical/agent-vault/internal/traces"
)

type shutdownTraceExporter struct {
	mu       sync.Mutex
	exported int
	stopped  bool
}

func (e *shutdownTraceExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exported += len(spans)
	return nil
}

func (e *shutdownTraceExporter) Shutdown(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopped = true
	return nil
}

func TestStartFlushesTracesOnBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	exporter := &shutdownTraceExporter{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	tr := traces.NewFromProvider(provider, "")
	_, span := tr.Start(context.Background(), "pending", trace.SpanKindServer)
	span.End("")
	s := &Server{httpServer: &http.Server{Addr: listener.Addr().String(), ReadHeaderTimeout: time.Second}, logger: slog.New(slog.DiscardHandler), traces: tr}
	if err := s.Start(); err == nil {
		t.Fatal("expected bind failure")
	}
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	if exporter.exported != 1 || !exporter.stopped {
		t.Fatal("Start returned without flushing and stopping traces")
	}
}
