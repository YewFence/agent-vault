// Package metrics contains Agent Vault's stable OpenTelemetry metrics contract.
package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/Infisical/agent-vault/internal/brokercore"
	"github.com/Infisical/agent-vault/internal/otlp"
)

const meterName = "agent_vault.proxy"

type ProxyEvent struct {
	Method, StatusClass, ErrorCode, ActorType, MatchedService string
	Status                                                    int
	Latency                                                   time.Duration
}

type Metrics struct {
	provider        *sdkmetric.MeterProvider
	requestsCounter metric.Int64Counter
	errorsCounter   metric.Int64Counter
	denialsCounter  metric.Int64Counter
	duration        metric.Float64Histogram
	inFlight        metric.Int64UpDownCounter
	exporterFailure metric.Int64Counter
}

type countingExporter struct {
	sdkmetric.Exporter
	onFailure func()
}

func (e *countingExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	if err := e.Exporter.Export(ctx, data); err != nil {
		if e.onFailure != nil {
			e.onFailure()
		}
		return err
	}
	return nil
}

// NewFromProvider builds the instruments on an already configured provider.
// Tests use this with sdkmetric.ManualReader; production uses NewFromEnv.
func NewFromProvider(provider *sdkmetric.MeterProvider) (*Metrics, error) {
	if provider == nil {
		return nil, nil
	}
	m := provider.Meter(meterName)
	rq, err := m.Int64Counter("agent_vault.proxy.requests")
	if err != nil {
		return nil, err
	}
	er, err := m.Int64Counter("agent_vault.proxy.errors")
	if err != nil {
		return nil, err
	}
	dn, err := m.Int64Counter("agent_vault.proxy.denials")
	if err != nil {
		return nil, err
	}
	dur, err := m.Float64Histogram("agent_vault.proxy.duration", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30))
	if err != nil {
		return nil, err
	}
	flight, err := m.Int64UpDownCounter("agent_vault.proxy.in_flight")
	if err != nil {
		return nil, err
	}
	failures, err := m.Int64Counter("agent_vault.exporter.failures")
	if err != nil {
		return nil, err
	}
	return &Metrics{provider: provider, requestsCounter: rq, errorsCounter: er, denialsCounter: dn, duration: dur, inFlight: flight, exporterFailure: failures}, nil
}

// NewFromEnv creates an OTLP metrics provider from the shared OTEL_*
// configuration. No endpoint (generic or metrics-specific) means disabled.
func NewFromEnv(version string) (*Metrics, error) {
	if !otlp.Enabled("metrics") {
		return nil, nil
	}
	ctx := context.Background()
	protocol, err := otlp.Protocol("metrics")
	if err != nil {
		return nil, err
	}
	var exp sdkmetric.Exporter
	switch protocol {
	case otlp.ProtocolGRPC:
		exp, err = otlpmetricgrpc.New(ctx)
	case otlp.ProtocolHTTPProtobuf:
		exp, err = otlpmetrichttp.New(ctx)
	}
	if err != nil {
		return nil, err
	}
	counted := &countingExporter{Exporter: exp}
	reader := sdkmetric.NewPeriodicReader(counted)
	res, err := otlp.NewResource(ctx, version)
	if err != nil {
		return nil, err
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
	m, err := NewFromProvider(provider)
	if err != nil {
		return nil, err
	}
	// The reader owns the wrapper; install the callback after instruments exist.
	counted.onFailure = func() { m.RecordExporterFailure(context.Background(), "metrics") }
	return m, nil
}

func attrs(e ProxyEvent) []attribute.KeyValue {
	return []attribute.KeyValue{attribute.String("http.request.method", e.Method), attribute.String("agent_vault.status_class", statusClass(e.Status)), attribute.String("agent_vault.error_code", e.ErrorCode), attribute.String("agent_vault.actor_type", e.ActorType), attribute.String("agent_vault.matched_service", service(e.MatchedService))}
}
func statusClass(s int) string {
	if s < 100 {
		return "unknown"
	}
	return string(rune('0'+s/100)) + "xx"
}
func service(s string) string {
	if s == "" {
		return "unmatched"
	}
	return s
}

func addOptions(attrs []attribute.KeyValue) []metric.AddOption {
	opts := make([]metric.AddOption, 0, len(attrs))
	for _, attr := range attrs {
		opts = append(opts, metric.WithAttributes(attr))
	}
	return opts
}

func recordOptions(attrs []attribute.KeyValue) []metric.RecordOption {
	opts := make([]metric.RecordOption, 0, len(attrs))
	for _, attr := range attrs {
		opts = append(opts, metric.WithAttributes(attr))
	}
	return opts
}

func (m *Metrics) Record(ctx context.Context, e ProxyEvent) {
	if m == nil {
		return
	}
	a := attrs(e)
	m.requestsCounter.Add(ctx, 1, addOptions(a)...)
	m.duration.Record(ctx, e.Latency.Seconds(), recordOptions(a)...)
	if e.ErrorCode != "" {
		if brokercore.IsDenial(e.ErrorCode) {
			m.denialsCounter.Add(ctx, 1, addOptions(a)...)
		} else {
			m.errorsCounter.Add(ctx, 1, addOptions(a)...)
		}
	}
}
func (m *Metrics) AddInFlight(ctx context.Context, delta int64, actorType string) {
	if m != nil {
		m.inFlight.Add(ctx, delta, addOptions([]attribute.KeyValue{attribute.String("agent_vault.actor_type", actorType)})...)
	}
}

// RecordExporterFailure counts one failed OTLP export for the given
// signal ("metrics", "logs"). Other signal packages report through this
// counter so exporter health stays a single time series sliced by signal.
func (m *Metrics) RecordExporterFailure(ctx context.Context, signal string) {
	if m == nil {
		return
	}
	m.exporterFailure.Add(ctx, 1, addOptions([]attribute.KeyValue{attribute.String("signal", signal)})...)
}

func (m *Metrics) Shutdown(ctx context.Context) error {
	if m == nil || m.provider == nil {
		return nil
	}
	return m.provider.Shutdown(ctx)
}
func (m *Metrics) Provider() *sdkmetric.MeterProvider {
	if m == nil {
		return nil
	}
	return m.provider
}
