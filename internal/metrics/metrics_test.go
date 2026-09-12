package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func testMetrics(t *testing.T) (*Metrics, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	m, err := NewFromProvider(provider)
	if err != nil {
		t.Fatalf("NewFromProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return m, reader
}

func TestRecordAndInFlight(t *testing.T) {
	m, reader := testMetrics(t)
	ctx := context.Background()
	m.AddInFlight(ctx, 1, "agent")
	m.Record(ctx, ProxyEvent{
		Method:         "GET",
		Status:         502,
		ErrorCode:      "upstream_error",
		ActorType:      "agent",
		MatchedService: "github",
		Latency:        250 * time.Millisecond,
	})
	m.AddInFlight(ctx, -1, "agent")

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(data.ScopeMetrics) != 1 {
		t.Fatalf("scope metrics = %d, want 1", len(data.ScopeMetrics))
	}
	if got := len(data.ScopeMetrics[0].Metrics); got != 4 {
		t.Fatalf("metrics = %d, want 4 populated instruments", got)
	}
	for _, item := range data.ScopeMetrics[0].Metrics {
		if item.Name == "agent_vault.proxy.errors" {
			if sum, ok := item.Data.(metricdata.Sum[int64]); !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
				t.Fatalf("errors metric = %#v, want one sample", item.Data)
			}
		}
	}
}

func TestDenialClassification(t *testing.T) {
	for _, code := range []string{"auth_failed", "ssrf_blocked", "rate_limit_scope", "no_match", "service_disabled"} {
		if !denial(code) {
			t.Errorf("denial(%q) = false", code)
		}
	}
	for _, code := range []string{"upstream_error", "credential_not_found", "internal", "substitution_error"} {
		if denial(code) {
			t.Errorf("denial(%q) = true", code)
		}
	}
}

type failingExporter struct{ err error }

func (failingExporter) Temporality(metric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}
func (failingExporter) Aggregation(metric.InstrumentKind) metric.Aggregation {
	return metric.AggregationDefault{}
}
func (failingExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return errors.New("export failed")
}
func (failingExporter) ForceFlush(context.Context) error { return nil }
func (failingExporter) Shutdown(context.Context) error   { return nil }

func TestCountingExporterCallsFailure(t *testing.T) {
	called := 0
	e := &countingExporter{Exporter: failingExporter{}, onFailure: func() { called++ }}
	if err := e.Export(context.Background(), &metricdata.ResourceMetrics{}); err == nil {
		t.Fatal("Export error = nil, want error")
	}
	if called != 1 {
		t.Fatalf("failure callback count = %d, want 1", called)
	}
}
