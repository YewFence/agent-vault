package otlp

import (
	"context"
	"testing"
)

// scrubEndpoints pins every endpoint/protocol variable to empty so the
// developer's shell cannot leak OTEL_* config into a case.
func scrubEndpoints(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"OTEL_SDK_DISABLED",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
	} {
		t.Setenv(v, "")
	}
}

func TestEnabled(t *testing.T) {
	scrubEndpoints(t)
	if Enabled("metrics") || Enabled("logs") || Enabled("traces") {
		t.Fatal("no endpoint set: Enabled = true, want false")
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4317")
	if !Enabled("metrics") || !Enabled("logs") || !Enabled("traces") {
		t.Fatal("generic endpoint set: Enabled = false, want true for both signals")
	}

	t.Setenv("OTEL_SDK_DISABLED", "true")
	if Enabled("metrics") || Enabled("logs") || Enabled("traces") {
		t.Fatal("OTEL_SDK_DISABLED=true: Enabled = true, want false")
	}
}

func TestEnabledPerSignalEndpoint(t *testing.T) {
	scrubEndpoints(t)
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://collector:4317")
	if Enabled("metrics") {
		t.Error("metrics: Enabled = true with only the logs endpoint set")
	}
	if !Enabled("logs") {
		t.Error("logs: Enabled = false with the per-signal endpoint set")
	}
}

func TestProtocol(t *testing.T) {
	scrubEndpoints(t)

	if p, err := Protocol("metrics"); err != nil || p != ProtocolGRPC {
		t.Fatalf("default = %q, %v; want grpc", p, err)
	}

	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	if p, err := Protocol("logs"); err != nil || p != ProtocolHTTPProtobuf {
		t.Fatalf("generic http/protobuf = %q, %v", p, err)
	}

	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "grpc")
	if p, err := Protocol("logs"); err != nil || p != ProtocolGRPC {
		t.Fatalf("per-signal override = %q, %v; want grpc", p, err)
	}
	if p, err := Protocol("metrics"); err != nil || p != ProtocolHTTPProtobuf {
		t.Fatalf("metrics should keep generic = %q, %v", p, err)
	}

	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "json")
	if _, err := Protocol("metrics"); err == nil {
		t.Fatal("unsupported protocol: err = nil")
	}
}

func TestNewResource(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	res, err := NewResource(context.Background(), "1.2.3")
	if err != nil {
		t.Fatalf("NewResource: %v", err)
	}
	attrs := make(map[string]string)
	for _, kv := range res.Attributes() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	if attrs["service.name"] != "agent-vault" {
		t.Errorf("service.name = %q, want agent-vault", attrs["service.name"])
	}
	if attrs["service.version"] != "1.2.3" {
		t.Errorf("service.version = %q, want 1.2.3", attrs["service.version"])
	}
	if attrs["service.instance.id"] == "" {
		t.Error("service.instance.id is empty")
	}
}
