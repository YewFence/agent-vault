// Package otlp resolves the standard OTEL_* exporter configuration
// shared by Agent Vault's metrics, logs, and traces and builds the
// resource they export under. Holding gating, protocol selection,
// and resource construction in one place keeps every signal behaving
// identically.
package otlp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/Infisical/agent-vault/internal/telemetry"
)

// Exporter protocols accepted in OTEL_EXPORTER_OTLP_*_PROTOCOL.
const (
	ProtocolGRPC         = "grpc"
	ProtocolHTTPProtobuf = "http/protobuf"
)

// Enabled reports whether export of the given signal ("metrics", "logs", "traces")
// is on. OTEL_SDK_DISABLED kills everything; otherwise either the generic
// OTEL_EXPORTER_OTLP_ENDPOINT or the per-signal endpoint enables it.
func Enabled(signal string) bool {
	if disabled := os.Getenv("OTEL_SDK_DISABLED"); disabled == "true" || disabled == "1" {
		return false
	}
	return os.Getenv(signalEnv(signal, "ENDPOINT")) != "" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

// Protocol resolves the transport for a signal: the per-signal
// OTEL_EXPORTER_OTLP_<SIGNAL>_PROTOCOL wins over the generic
// OTEL_EXPORTER_OTLP_PROTOCOL, defaulting to grpc.
func Protocol(signal string) (string, error) {
	p := strings.ToLower(os.Getenv(signalEnv(signal, "PROTOCOL")))
	if p == "" {
		p = strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))
	}
	switch p {
	case "", ProtocolGRPC:
		return ProtocolGRPC, nil
	case ProtocolHTTPProtobuf, "http":
		return ProtocolHTTPProtobuf, nil
	default:
		return "", fmt.Errorf("unsupported OTLP protocol %q (use grpc or http/protobuf)", p)
	}
}

// NewResource builds the resource shared by every signal: stable service
// identity plus an anonymous machine instance ID. OTEL_SERVICE_NAME and
// OTEL_RESOURCE_ATTRIBUTES override via the environment.
func NewResource(ctx context.Context, version string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("agent-vault"),
			semconv.ServiceVersion(version),
			attribute.String("service.instance.id", telemetry.MachineID()),
		),
		resource.WithFromEnv(),
	)
}

func signalEnv(signal, suffix string) string {
	return "OTEL_EXPORTER_OTLP_" + strings.ToUpper(signal) + "_" + suffix
}
