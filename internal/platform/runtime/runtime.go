package runtime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	botpostgres "github.com/dmuraveiko/RW/internal/bot/adapter/postgres"
	"github.com/dmuraveiko/RW/internal/bot/adapter/telegram"
	botapp "github.com/dmuraveiko/RW/internal/bot/app"
	"github.com/dmuraveiko/RW/internal/bot/presentation"
	"github.com/dmuraveiko/RW/internal/platform/config"
	platformcrypto "github.com/dmuraveiko/RW/internal/platform/crypto"
	"github.com/dmuraveiko/RW/internal/platform/message"
	"github.com/dmuraveiko/RW/internal/platform/messaging"
	"github.com/dmuraveiko/RW/internal/platform/migrate"
	"github.com/dmuraveiko/RW/internal/platform/observability"
	"github.com/dmuraveiko/RW/internal/platform/postgres"
	sessionspostgres "github.com/dmuraveiko/RW/internal/sessions/adapter/postgres"
	sessionsapp "github.com/dmuraveiko/RW/internal/sessions/app"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Service = config.Service

const (
	Bot            = config.Bot
	ActiveSessions = config.ActiveSessions
)

func Run(parent context.Context, service Service) error {
	cfg, err := config.Load(service)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if service == Bot && (cfg.Bot.TelegramMode == "direct_webhook" || cfg.Bot.TelegramMode == "natsproxy") {
		return fmt.Errorf("telegram mode %s is not implemented", cfg.Bot.TelegramMode)
	}
	logger := observability.NewLogger(string(service), string(cfg.Environment), cfg.InstanceID, cfg.LogLevel)
	metrics := observability.NewMetrics()

	security, err := loadSecurityMaterial(cfg, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("load security material: %w", err)
	}
	pool, err := postgres.Open(parent, cfg.Database, logger)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = migrate.CheckVersion(parent, pool, config.SchemaVersion(service)); err != nil {
		return err
	}
	connection, err := messaging.Connect(service, cfg.NATS, logger, metrics)
	if err != nil {
		return err
	}
	defer connection.Close()

	app := &application{cfg: cfg, logger: logger, metrics: metrics, pool: pool, nats: connection}
	if service == Bot {
		store := botpostgres.NewFlowStore(pool, security.dataKeyring, security.fingerprintKey)
		codec := message.Codec{Producer: string(service), KeyID: cfg.Security.SigningKeyID, PrivateKey: security.privateKey, Trusted: security.trustedKeys, ClockSkew: config.ClockSkew}
		var client *telegram.Client
		var botMessenger botapp.Messenger
		if cfg.Bot.TelegramMode == "direct_polling" {
			client, err = telegram.NewClient(cfg.Bot.TelegramToken, cfg.Bot.PollTimeout+15*time.Second)
			if err != nil {
				return err
			}
			botMessenger = client
		}
		activation := botapp.NewActivationService(store, codec, cfg, logger, connection, botMessenger, func() { app.domainReady.Store(true) })
		workers := []func(context.Context) error{activation.Run}
		if client != nil {
			updates := botpostgres.NewUpdateStore(pool, "direct_polling")
			handler := botapp.NewFlowMessageHandler(client, updates, presentation.Russian{}, activation)
			poller := telegram.NewPoller(client, handler, logger, cfg.Bot.PollTimeout, cfg.Bot.TelegramBotID, cfg.Bot.TelegramBotUsername, func(identity telegram.User) {
				activation.SetTelegramIdentity(identity.ID, identity.Username)
				app.telegramReady.Store(true)
			})
			workers = append(workers, poller.Run)
		}
		app.worker = runWorkers(workers...)
	}
	if service == ActiveSessions {
		store := sessionspostgres.NewStore(pool, security.dataKeyring, security.fingerprintKey)
		codec := message.Codec{Producer: string(service), KeyID: cfg.Security.SigningKeyID, PrivateKey: security.privateKey, Trusted: security.trustedKeys, ClockSkew: config.ClockSkew}
		sessions := sessionsapp.NewService(store, codec, cfg, logger, connection, func() { app.domainReady.Store(true) })
		app.worker = sessions.Run
	}
	return app.serve(parent)
}

type application struct {
	cfg           config.Config
	logger        *slog.Logger
	metrics       *observability.Metrics
	pool          *pgxpool.Pool
	nats          *nats.Conn
	worker        func(context.Context) error
	telegramReady atomic.Bool
	domainReady   atomic.Bool
}

func (a *application) serve(parent context.Context) error {
	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mainMux := http.NewServeMux()
	mainMux.HandleFunc("GET /health/live", liveHandler)
	mainMux.HandleFunc("GET /health/ready", a.readyHandler)
	mainServer := server(a.cfg.HTTPAddr, mainMux)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", promhttp.HandlerFor(a.metrics.Registry, promhttp.HandlerOpts{}))
	metricsServer := server(a.cfg.MetricsAddr, metricsMux)

	errorsChannel := make(chan error, 3)
	go listen(mainServer, errorsChannel)
	go listen(metricsServer, errorsChannel)
	if a.worker != nil {
		go func() { errorsChannel <- a.worker(ctx) }()
	}
	a.metrics.Ready.Set(1)
	a.logger.Info("service started", "http_addr", a.cfg.HTTPAddr, "metrics_addr", a.cfg.MetricsAddr)

	var runErr error
	select {
	case <-ctx.Done():
		a.logger.Info("shutdown requested")
	case err := <-errorsChannel:
		stop()
		runErr = err
		if err != nil {
			a.logger.Error("HTTP server stopped unexpectedly", "error", err)
		}
	}
	a.metrics.Ready.Set(0)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Shutdown)
	defer cancel()
	if err := mainServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown metrics server: %w", err)
	}
	if err := a.nats.Drain(); err != nil && !errors.Is(err, nats.ErrConnectionClosed) {
		return fmt.Errorf("drain NATS: %w", err)
	}
	a.logger.Info("service stopped")
	return runErr
}

func server(address string, handler http.Handler) *http.Server {
	return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
}

func listen(server *http.Server, result chan<- error) {
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	result <- err
}

func liveHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte("{\"status\":\"ok\"}\n"))
}

func (a *application) readyHandler(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := a.checkReady(ctx); err != nil {
		a.metrics.Ready.Set(0)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("{\"status\":\"not_ready\"}\n"))
		return
	}
	a.metrics.Ready.Set(1)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte("{\"status\":\"ready\"}\n"))
}

func (a *application) checkReady(ctx context.Context) error {
	started := time.Now()
	err := a.pool.Ping(ctx)
	a.metrics.DependencyCheckTime.WithLabelValues("postgres").Observe(time.Since(started).Seconds())
	if err != nil {
		return errors.New("database unavailable")
	}
	if err = migrate.CheckVersion(ctx, a.pool, config.SchemaVersion(a.cfg.Service)); err != nil {
		return errors.New("database schema incompatible")
	}
	if !a.nats.IsConnected() {
		return errors.New("NATS unavailable")
	}
	if a.cfg.Service == Bot && a.cfg.Bot.TelegramMode == "direct_polling" && !a.telegramReady.Load() {
		return errors.New("telegram polling unavailable")
	}
	if a.cfg.Service == Bot && !a.domainReady.Load() {
		return errors.New("bot consumers unavailable")
	}
	if a.cfg.Service == ActiveSessions && !a.domainReady.Load() {
		return errors.New("active-sessions consumers unavailable")
	}
	return nil
}

func runWorkers(workers ...func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		results := make(chan error, len(workers))
		for _, worker := range workers {
			worker := worker
			go func() { results <- worker(workerCtx) }()
		}
		select {
		case <-ctx.Done():
			cancel()
			for range workers {
				<-results
			}
			return nil
		case err := <-results:
			cancel()
			return err
		}
	}
}

type securityMaterial struct {
	privateKey     ed25519.PrivateKey
	trustedKeys    platformcrypto.TrustedKeys
	dataKeyring    platformcrypto.DataKeyring
	fingerprintKey []byte
}

func loadSecurityMaterial(cfg config.Config, now time.Time) (securityMaterial, error) {
	var material securityMaterial
	privateKey, err := platformcrypto.LoadSigningPrivateKey(cfg.Security.SigningPrivateKeyFile, cfg.Security.SigningPrivateKey)
	if err != nil {
		return material, err
	}
	trusted, err := platformcrypto.LoadTrustedKeys(cfg.Security.TrustedKeysFile)
	if err != nil {
		return material, err
	}
	publicKey, err := trusted.Lookup(string(cfg.Service), cfg.Security.SigningKeyID, now)
	if err != nil {
		return material, fmt.Errorf("current signing key is not trusted: %w", err)
	}
	if !bytes.Equal(publicKey, privateKey.Public().(ed25519.PublicKey)) {
		return material, errors.New("signing private key does not match trusted public key")
	}
	if (cfg.Environment == config.Staging || cfg.Environment == config.Prod) && strings.Contains(strings.ToLower(cfg.Security.SigningKeyID), "test") {
		return material, errors.New("test signing key is forbidden in staging/prod")
	}
	dataKeyring, err := platformcrypto.LoadDataKeyring(cfg.Security.DataKeyringFile)
	if err != nil {
		return material, err
	}
	fingerprintKey, err := platformcrypto.LoadFingerprintKey(cfg.Security.FingerprintKeyFile)
	if err != nil {
		return material, err
	}
	material = securityMaterial{privateKey: privateKey, trustedKeys: trusted, dataKeyring: dataKeyring, fingerprintKey: fingerprintKey}
	return material, nil
}
