package server

import (
	"context"
	"time"
)

// Flush on every exit from Start, including bind errors and HTTP shutdown
// timeouts. Use a fresh deadline so draining HTTP cannot exhaust the budget.
func (s *Server) shutdownObservability() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.traces.Shutdown(ctx); err != nil {
		s.logger.Warn("traces shutdown failed", "err", err)
	}
	if err := s.otlpLogs.Shutdown(ctx); err != nil {
		s.logger.Warn("logs shutdown failed", "err", err)
	}
	// Keep exporter-failure instruments alive until other signals flush.
	if err := s.metrics.Shutdown(ctx); err != nil {
		s.logger.Warn("metrics shutdown failed", "err", err)
	}
}
