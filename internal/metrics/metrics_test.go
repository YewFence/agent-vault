package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
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
			attrs := attrMap(item.Data.(metricdata.Sum[int64]).DataPoints[0].Attributes)
			want := map[string]string{
				"http.request.method":        "GET",
				"agent_vault.status_class":   "5xx",
				"agent_vault.error_code":     "upstream_error",
				"agent_vault.actor_type":     "agent",
				"agent_vault.matched_service": "github",
			}
			for k, v := range want {
				if attrs[k] != v {
					t.Errorf("attribute %q = %q, want %q", k, attrs[k], v)
				}
			}
		}
	}
}

func attrMap(set attribute.Set) map[string]string {
	out := make(map[string]string, set.Len())
	for _, kv := range set.ToSlice() {
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}

func TestRecordExporterFailure(t *testing.T) {
	m, reader := testMetrics(t)
	ctx := context.Background()
	m.RecordExporterFailure(ctx, "logs")

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 1 {
		t.Fatalf("populated metrics = %#v, want exactly exporter.failures", data.ScopeMetrics)
	}
	item := data.ScopeMetrics[0].Metrics[0]
	if item.Name != "agent_vault.exporter.failures" {
		t.Fatalf("metric = %q, want agent_vault.exporter.failures", item.Name)
	}
	sum, ok := item.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
		t.Fatalf("exporter.failures = %#v, want one sample of 1", item.Data)
	}
	if got := attrMap(sum.DataPoints[0].Attributes)["signal"]; got != "logs" {
		t.Fatalf("signal attribute = %q, want logs", got)
	}
}

type failingExporter struct{}

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
