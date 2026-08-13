package messaging

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/dmuraveiko/RW/internal/platform/config"
	"github.com/dmuraveiko/RW/internal/platform/observability"
	"github.com/nats-io/nats.go"
)

func Connect(service config.Service, cfg config.NATS, logger *slog.Logger, metrics *observability.Metrics) (*nats.Conn, error) {
	options := []nats.Option{
		nats.Name(string(service)), nats.Timeout(cfg.ConnectTimeout),
		nats.ReconnectWait(cfg.ReconnectWait), nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectBufSize(cfg.ReconnectBuffer),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			metrics.NATSConnected.Set(0)
			logger.Warn("NATS disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(connection *nats.Conn) {
			metrics.NATSConnected.Set(1)
			metrics.NATSReconnects.Inc()
			logger.Info("NATS reconnected", "server", connection.ConnectedUrlRedacted())
		}),
		nats.ClosedHandler(func(connection *nats.Conn) {
			metrics.NATSConnected.Set(0)
			logger.Info("NATS connection closed", "error", connection.LastError())
		}),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			metrics.NATSErrors.WithLabelValues(errorKind(err)).Inc()
			logger.Error("NATS asynchronous error", "kind", errorKind(err))
		}),
	}
	if cfg.CredentialsFile != "" {
		options = append(options, nats.UserCredentials(cfg.CredentialsFile))
	}
	if cfg.TLSCAFile != "" {
		tlsConfig, err := tlsConfig(cfg)
		if err != nil {
			return nil, err
		}
		options = append(options, nats.Secure(tlsConfig))
	}
	connection, err := nats.Connect(strings.Join(cfg.URLs, ","), options...)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}
	metrics.NATSConnected.Set(1)
	logger.Info("NATS connection established", "server", connection.ConnectedUrlRedacted())
	return connection, nil
}

func SetPendingLimits(subscription *nats.Subscription, cfg config.NATS) error {
	if err := subscription.SetPendingLimits(cfg.PendingMessages, cfg.PendingBytes); err != nil {
		return fmt.Errorf("set NATS pending limits: %w", err)
	}
	return nil
}

func tlsConfig(cfg config.NATS) (*tls.Config, error) {
	contents, err := os.ReadFile(cfg.TLSCAFile)
	if err != nil {
		return nil, fmt.Errorf("read NATS CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(contents) {
		return nil, errors.New("NATS CA file contains no certificates")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: cfg.TLSServerName}, nil
}

func errorKind(err error) string {
	if errors.Is(err, nats.ErrSlowConsumer) {
		return "slow_consumer"
	}
	return "async"
}
