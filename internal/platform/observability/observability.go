package observability

import (
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

type Metrics struct {
	Registry            *prometheus.Registry
	Ready               prometheus.Gauge
	NATSConnected       prometheus.Gauge
	NATSReconnects      prometheus.Counter
	NATSErrors          *prometheus.CounterVec
	DependencyCheckTime *prometheus.HistogramVec
}

func NewLogger(service, environment, instanceID, level string) *slog.Logger {
	var parsed slog.Level
	switch level {
	case "debug":
		parsed = slog.LevelDebug
	case "warn":
		parsed = slog.LevelWarn
	case "error":
		parsed = slog.LevelError
	default:
		parsed = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parsed})
	return slog.New(handler).With("service", service, "environment", environment, "instance_id", instanceID)
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		Registry:            registry,
		Ready:               prometheus.NewGauge(prometheus.GaugeOpts{Name: "rw_ready", Help: "Whether the service is ready to receive work."}),
		NATSConnected:       prometheus.NewGauge(prometheus.GaugeOpts{Name: "rw_nats_connected", Help: "Whether the NATS connection is active."}),
		NATSReconnects:      prometheus.NewCounter(prometheus.CounterOpts{Name: "rw_nats_reconnects_total", Help: "Total successful NATS reconnects."}),
		NATSErrors:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "rw_nats_errors_total", Help: "NATS asynchronous errors."}, []string{"kind"}),
		DependencyCheckTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "rw_dependency_check_duration_seconds", Help: "Dependency readiness check duration.", Buckets: prometheus.DefBuckets}, []string{"dependency"}),
	}
	registry.MustRegister(metrics.Ready, metrics.NATSConnected, metrics.NATSReconnects, metrics.NATSErrors, metrics.DependencyCheckTime)
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return metrics
}
