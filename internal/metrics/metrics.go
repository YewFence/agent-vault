// Package metrics contains Agent Vault's stable OpenTelemetry metrics contract.
package metrics

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/Infisical/agent-vault/internal/telemetry"
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
	return &Metrics{provider: provider, requestsCounter: rq, errorsCounter: er, denialsCounter: dn, duration: dur, inFlight: flight}, nil
}

// NewFromEnv creates an OTLP metrics provider. No endpoint means disabled.
func NewFromEnv(version string) (*Metrics, error) {
	if os.Getenv("OTEL_SDK_DISABLED") == "true" || os.Getenv("OTEL_SDK_DISABLED") == "1" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return nil, nil
	}
	ctx := context.Background()
	protocol := strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))
	var reader sdkmetric.Reader
	var err error
	if protocol == "grpc" || protocol == "" {
		var exp sdkmetric.Exporter
		exp, err = otlpmetricgrpc.New(ctx)
		if err == nil {
			reader = sdkmetric.NewPeriodicReader(exp)
		}
	} else if protocol == "http/protobuf" || protocol == "http" {
		var exp sdkmetric.Exporter
		exp, err = otlpmetrichttp.New(ctx)
		if err == nil {
			reader = sdkmetric.NewPeriodicReader(exp)
		}
	} else {
		return nil, errors.New("unsupported OTEL_EXPORTER_OTLP_PROTOCOL (use grpc or http/protobuf)")
	}
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName("agent-vault"), semconv.ServiceVersion(version), attribute.String("service.instance.id", anonymousInstanceID())),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, err
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
	return NewFromProvider(provider)
}

func anonymousInstanceID() string { return telemetry.MachineID() }
func attrs(e ProxyEvent) []attribute.KeyValue {
	return []attribute.KeyValue{attribute.String("method", e.Method), attribute.String("status_class", statusClass(e.Status)), attribute.String("error_code", e.ErrorCode), attribute.String("actor_type", e.ActorType), attribute.String("matched_service", service(e.MatchedService))}
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
func denial(code string) bool {
	return strings.Contains(code, "auth") ||
		strings.Contains(code, "ssrf") ||
		strings.Contains(code, "rate") ||
		strings.Contains(code, "policy") ||
		code == "no_match" ||
		code == "service_disabled"
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
		if denial(e.ErrorCode) {
			m.denialsCounter.Add(ctx, 1, addOptions(a)...)
		} else {
			m.errorsCounter.Add(ctx, 1, addOptions(a)...)
		}
	}
}
func (m *Metrics) AddInFlight(ctx context.Context, delta int64, actorType string) {
	if m != nil {
		m.inFlight.Add(ctx, delta, addOptions([]attribute.KeyValue{attribute.String("actor_type", actorType)})...)
	}
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
