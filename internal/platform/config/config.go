package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Service string

const (
	Bot            Service = "rw-bot"
	ActiveSessions Service = "rw-active-sessions"
)

type Environment string

const (
	Local   Environment = "local"
	Test    Environment = "test"
	Staging Environment = "staging"
	Prod    Environment = "prod"
)

type Config struct {
	Service     Service
	Environment Environment
	InstanceID  string
	LogLevel    string
	HTTPAddr    string
	MetricsAddr string
	Shutdown    time.Duration
	Database    Database
	NATS        NATS
	Security    Security
	Bot         BotConfig
	Sessions    SessionsConfig
}

type Database struct {
	URL              string
	PoolMax          int32
	PoolMin          int32
	StatementTimeout time.Duration
	LockTimeout      time.Duration
	IdleTxTimeout    time.Duration
}

type NATS struct {
	URLs            []string
	CredentialsFile string
	TLSCAFile       string
	TLSServerName   string
	ConnectTimeout  time.Duration
	ReconnectWait   time.Duration
	MaxReconnects   int
	ReconnectBuffer int
	PendingMessages int
	PendingBytes    int
}

type Security struct {
	SigningKeyID          string
	SigningPrivateKeyFile string
	SigningPrivateKey     string
	TrustedKeysFile       string
	DataKeyringFile       string
	FingerprintKeyFile    string
}

type BotConfig struct {
	TelegramMode        string
	TelegramBotID       int64
	TelegramBotUsername string
	TelegramTokenFile   string
	TelegramToken       string
	WebhookPublicURL    string
	WebhookSecretFile   string
	PollTimeout         time.Duration
	InviteTTL           time.Duration
	USDTNetwork         string
	USDTScale           int
	ActivationAmountMin int64
	ActivationAmountMax int64
}

type SessionsConfig struct {
	USDTNetwork string
	PageSize    int
}

const (
	ClockSkew       = 2 * time.Minute
	MessageMaxBytes = 262144
)

func SchemaVersion(_ Service) int64 {
	return 1
}

var allowed = map[string]struct{}{
	"RW_ENVIRONMENT": {}, "RW_INSTANCE_ID": {}, "RW_LOG_LEVEL": {},
	"RW_HTTP_ADDR": {}, "RW_METRICS_ADDR": {}, "RW_SHUTDOWN_TIMEOUT": {},
	"RW_OTEL_EXPORTER_OTLP_ENDPOINT": {}, "RW_OTEL_SAMPLE_RATIO": {},
	"RW_DATABASE_URL_FILE": {}, "RW_DATABASE_URL": {}, "RW_DB_POOL_MAX": {},
	"RW_DB_POOL_MIN": {}, "RW_DB_STATEMENT_TIMEOUT": {}, "RW_DB_LOCK_TIMEOUT": {},
	"RW_DB_IDLE_TX_TIMEOUT": {}, "RW_NATS_URLS": {}, "RW_NATS_CREDS_FILE": {},
	"RW_NATS_TLS_CA_FILE": {}, "RW_NATS_TLS_SERVER_NAME": {},
	"RW_NATS_CONNECT_TIMEOUT": {}, "RW_NATS_RECONNECT_WAIT": {},
	"RW_NATS_MAX_RECONNECTS": {}, "RW_NATS_RECONNECT_BUFFER": {},
	"RW_NATS_PENDING_MESSAGES": {}, "RW_NATS_PENDING_BYTES": {},
	"RW_SIGNING_KEY_ID": {}, "RW_SIGNING_PRIVATE_KEY_FILE": {},
	"RW_SIGNING_PRIVATE_KEY_BASE64": {}, "RW_TRUSTED_KEYS_FILE": {},
	"RW_DATA_KEYRING_FILE": {}, "RW_FINGERPRINT_KEY_FILE": {},
	"RW_CLOCK_SKEW": {}, "RW_MESSAGE_MAX_BYTES": {},
	"RW_TELEGRAM_MODE": {}, "RW_TELEGRAM_BOT_ID": {},
	"RW_TELEGRAM_BOT_USERNAME": {}, "RW_TELEGRAM_TOKEN_FILE": {},
	"RW_TELEGRAM_TOKEN": {}, "RW_TELEGRAM_WEBHOOK_PUBLIC_URL": {},
	"RW_TELEGRAM_WEBHOOK_SECRET_FILE": {}, "RW_TELEGRAM_POLL_TIMEOUT": {},
	"RW_INVITE_TTL": {}, "RW_INVITE_TTL_MIN": {}, "RW_INVITE_TTL_MAX": {},
	"RW_USDT_NETWORK": {}, "RW_USDT_SCALE": {},
	"RW_ACTIVATION_AMOUNT_MIN_MINOR": {}, "RW_ACTIVATION_AMOUNT_MAX_MINOR": {},
	"RW_ACTIVATION_OFFER_MIN": {}, "RW_ACTIVATION_OFFER_MAX": {},
	"RW_OUTBOX_BATCH_SIZE": {}, "RW_OUTBOX_CONCURRENCY": {},
	"RW_OUTBOX_RETRY_INITIAL": {}, "RW_OUTBOX_RETRY_MAX": {},
	"RW_RECONCILE_INTERVAL": {}, "RW_RECONCILE_BATCH_SIZE": {},
	"RW_RETENTION_INTERVAL": {}, "RW_RETENTION_BATCH_SIZE": {},
	"RW_TELEGRAM_RATE_PER_MINUTE": {}, "RW_TELEGRAM_RATE_BURST": {},
	"RW_INVALID_INVITE_LIMIT": {}, "RW_INVALID_INVITE_WINDOW": {},
	"RW_INVALID_INVITE_BLOCK": {}, "RW_ACTIVATION_RATE_IDENTITY_HOURLY": {},
	"RW_ACTIVATION_RATE_BALANCE_HOURLY": {}, "RW_SESSION_LIST_PAGE_SIZE": {},
	"RW_SESSION_LIST_PAGE_MAX": {}, "RW_VERIFICATION_DEADLINE_MAX": {},
}

func Load(service Service) (Config, error) {
	if service != Bot && service != ActiveSessions {
		return Config{}, fmt.Errorf("unsupported service %q", service)
	}
	if err := rejectUnknown(); err != nil {
		return Config{}, err
	}

	environment := Environment(os.Getenv("RW_ENVIRONMENT"))
	if environment != Local && environment != Test && environment != Staging && environment != Prod {
		return Config{}, errors.New("RW_ENVIRONMENT must be local, test, staging or prod")
	}
	if err := fixed("RW_CLOCK_SKEW", "2m"); err != nil {
		return Config{}, err
	}
	if err := fixed("RW_MESSAGE_MAX_BYTES", "262144"); err != nil {
		return Config{}, err
	}

	databaseURL, err := secretValue("RW_DATABASE_URL", "RW_DATABASE_URL_FILE", environment, true)
	if err != nil {
		return Config{}, err
	}
	if err = validateDatabasePolicy(databaseURL, environment); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Service: service, Environment: environment,
		InstanceID: value("RW_INSTANCE_ID", hostname()),
		LogLevel:   value("RW_LOG_LEVEL", "info"), HTTPAddr: value("RW_HTTP_ADDR", ":8080"),
		MetricsAddr: value("RW_METRICS_ADDR", ":9090"),
	}
	if cfg.Shutdown, err = duration("RW_SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Database, err = loadDatabase(databaseURL); err != nil {
		return Config{}, err
	}
	if cfg.NATS, err = loadNATS(environment); err != nil {
		return Config{}, err
	}
	if cfg.Security, err = loadSecurity(environment); err != nil {
		return Config{}, err
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, errors.New("RW_LOG_LEVEL must be debug, info, warn or error")
	}
	if environment == Prod && strings.TrimSpace(os.Getenv("RW_INSTANCE_ID")) == "" {
		return Config{}, errors.New("RW_INSTANCE_ID is required in prod")
	}
	if cfg.HTTPAddr == cfg.MetricsAddr {
		return Config{}, errors.New("RW_HTTP_ADDR and RW_METRICS_ADDR must differ")
	}

	if service == Bot {
		cfg.Bot, err = loadBot(environment)
	} else {
		cfg.Sessions, err = loadSessions()
	}
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadDatabase(databaseURL string) (Database, error) {
	var err error
	cfg := Database{URL: databaseURL}
	if cfg.PoolMax, err = int32Value("RW_DB_POOL_MAX", 20); err != nil {
		return cfg, err
	}
	if cfg.PoolMin, err = int32Value("RW_DB_POOL_MIN", 2); err != nil {
		return cfg, err
	}
	if cfg.StatementTimeout, err = duration("RW_DB_STATEMENT_TIMEOUT", 5*time.Second); err != nil {
		return cfg, err
	}
	if cfg.LockTimeout, err = duration("RW_DB_LOCK_TIMEOUT", time.Second); err != nil {
		return cfg, err
	}
	if cfg.IdleTxTimeout, err = duration("RW_DB_IDLE_TX_TIMEOUT", 10*time.Second); err != nil {
		return cfg, err
	}
	if cfg.PoolMin < 0 || cfg.PoolMax < 1 || cfg.PoolMin > cfg.PoolMax {
		return cfg, errors.New("database pool sizes are invalid")
	}
	return cfg, nil
}

func loadNATS(environment Environment) (NATS, error) {
	var err error
	cfg := NATS{URLs: split(os.Getenv("RW_NATS_URLS")), CredentialsFile: os.Getenv("RW_NATS_CREDS_FILE"), TLSCAFile: os.Getenv("RW_NATS_TLS_CA_FILE"), TLSServerName: os.Getenv("RW_NATS_TLS_SERVER_NAME")}
	if len(cfg.URLs) == 0 {
		return cfg, errors.New("RW_NATS_URLS is required")
	}
	for _, raw := range cfg.URLs {
		u, parseErr := url.Parse(raw)
		if parseErr != nil || (u.Scheme != "nats" && u.Scheme != "tls") || u.Host == "" {
			return cfg, fmt.Errorf("invalid NATS URL %q", raw)
		}
		if u.User != nil {
			return cfg, errors.New("NATS credentials must not be embedded in URLs")
		}
		if (environment == Staging || environment == Prod) && u.Scheme != "tls" {
			return cfg, errors.New("NATS URLs must use tls:// in staging/prod")
		}
	}
	if environment == Staging || environment == Prod {
		if cfg.CredentialsFile == "" || cfg.TLSCAFile == "" || cfg.TLSServerName == "" {
			return cfg, errors.New("NATS credentials and TLS settings are required in staging/prod")
		}
	}
	for _, path := range []string{cfg.CredentialsFile, cfg.TLSCAFile} {
		if path != "" {
			if err = validateFile(path); err != nil {
				return cfg, err
			}
		}
	}
	if cfg.ConnectTimeout, err = duration("RW_NATS_CONNECT_TIMEOUT", 3*time.Second); err != nil {
		return cfg, err
	}
	if cfg.ReconnectWait, err = duration("RW_NATS_RECONNECT_WAIT", time.Second); err != nil {
		return cfg, err
	}
	if cfg.MaxReconnects, err = integer("RW_NATS_MAX_RECONNECTS", -1); err != nil {
		return cfg, err
	}
	if cfg.ReconnectBuffer, err = bytesValue("RW_NATS_RECONNECT_BUFFER", 8<<20); err != nil {
		return cfg, err
	}
	if cfg.PendingMessages, err = integer("RW_NATS_PENDING_MESSAGES", 4096); err != nil {
		return cfg, err
	}
	if cfg.PendingBytes, err = bytesValue("RW_NATS_PENDING_BYTES", 16<<20); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func loadSecurity(environment Environment) (Security, error) {
	cfg := Security{SigningKeyID: os.Getenv("RW_SIGNING_KEY_ID"), SigningPrivateKeyFile: os.Getenv("RW_SIGNING_PRIVATE_KEY_FILE"), SigningPrivateKey: os.Getenv("RW_SIGNING_PRIVATE_KEY_BASE64"), TrustedKeysFile: os.Getenv("RW_TRUSTED_KEYS_FILE"), DataKeyringFile: os.Getenv("RW_DATA_KEYRING_FILE"), FingerprintKeyFile: os.Getenv("RW_FINGERPRINT_KEY_FILE")}
	if cfg.SigningKeyID == "" || cfg.TrustedKeysFile == "" || cfg.DataKeyringFile == "" || cfg.FingerprintKeyFile == "" {
		return cfg, errors.New("signing key ID and security manifests are required")
	}
	if cfg.SigningPrivateKeyFile != "" && cfg.SigningPrivateKey != "" {
		return cfg, errors.New("configure only one signing private key source")
	}
	if environment == Staging || environment == Prod {
		if cfg.SigningPrivateKeyFile == "" || cfg.SigningPrivateKey != "" {
			return cfg, errors.New("signing private key file is required in staging/prod")
		}
	} else if cfg.SigningPrivateKeyFile == "" && cfg.SigningPrivateKey == "" {
		return cfg, errors.New("signing private key is required")
	}
	return cfg, nil
}

func loadBot(environment Environment) (BotConfig, error) {
	var err error
	cfg := BotConfig{TelegramMode: os.Getenv("RW_TELEGRAM_MODE"), TelegramBotUsername: os.Getenv("RW_TELEGRAM_BOT_USERNAME"), TelegramTokenFile: os.Getenv("RW_TELEGRAM_TOKEN_FILE"), TelegramToken: os.Getenv("RW_TELEGRAM_TOKEN"), WebhookPublicURL: os.Getenv("RW_TELEGRAM_WEBHOOK_PUBLIC_URL"), WebhookSecretFile: os.Getenv("RW_TELEGRAM_WEBHOOK_SECRET_FILE"), USDTNetwork: os.Getenv("RW_USDT_NETWORK")}
	if cfg.TelegramBotID, err = int64Value("RW_TELEGRAM_BOT_ID", 0); err != nil {
		return cfg, err
	}
	if cfg.PollTimeout, err = duration("RW_TELEGRAM_POLL_TIMEOUT", 30*time.Second); err != nil {
		return cfg, err
	}
	if cfg.InviteTTL, err = duration("RW_INVITE_TTL", 24*time.Hour); err != nil {
		return cfg, err
	}
	if cfg.USDTScale, err = integerRequired("RW_USDT_SCALE"); err != nil {
		return cfg, err
	}
	if cfg.ActivationAmountMin, err = int64Required("RW_ACTIVATION_AMOUNT_MIN_MINOR"); err != nil {
		return cfg, err
	}
	if cfg.ActivationAmountMax, err = int64Required("RW_ACTIVATION_AMOUNT_MAX_MINOR"); err != nil {
		return cfg, err
	}
	if cfg.USDTNetwork == "" {
		return cfg, errors.New("RW_USDT_NETWORK is required")
	}
	if cfg.USDTScale < 0 || cfg.USDTScale > 18 {
		return cfg, errors.New("RW_USDT_SCALE must be between 0 and 18")
	}
	if cfg.InviteTTL < 5*time.Minute || cfg.InviteTTL > 168*time.Hour {
		return cfg, errors.New("RW_INVITE_TTL must be between 5m and 168h")
	}
	if cfg.ActivationAmountMin < 0 || cfg.ActivationAmountMax < cfg.ActivationAmountMin || cfg.ActivationAmountMax-cfg.ActivationAmountMin < 999 {
		return cfg, errors.New("activation amount range must contain at least 1000 values")
	}
	for name, expected := range map[string]string{"RW_INVITE_TTL_MIN": "5m", "RW_INVITE_TTL_MAX": "168h", "RW_ACTIVATION_OFFER_MIN": "1m", "RW_ACTIVATION_OFFER_MAX": "60m"} {
		if err = fixed(name, expected); err != nil {
			return cfg, err
		}
	}
	switch cfg.TelegramMode {
	case "disabled":
		if environment != Local && environment != Test {
			return cfg, errors.New("disabled Telegram mode is allowed only in local/test")
		}
		if cfg.TelegramToken != "" || cfg.TelegramTokenFile != "" || cfg.WebhookPublicURL != "" || cfg.WebhookSecretFile != "" {
			return cfg, errors.New("telegram credentials and webhook settings are forbidden in disabled mode")
		}
	case "natsproxy":
		if cfg.TelegramToken != "" || cfg.TelegramTokenFile != "" || cfg.WebhookPublicURL != "" || cfg.WebhookSecretFile != "" {
			return cfg, errors.New("telegram credentials and webhook settings are forbidden in natsproxy mode")
		}
	case "direct_polling":
		if cfg.TelegramToken, err = secretValue("RW_TELEGRAM_TOKEN", "RW_TELEGRAM_TOKEN_FILE", environment, true); err != nil {
			return cfg, err
		}
		if cfg.WebhookPublicURL != "" || cfg.WebhookSecretFile != "" {
			return cfg, errors.New("webhook settings are forbidden in polling mode")
		}
	case "direct_webhook":
		if cfg.TelegramToken, err = secretValue("RW_TELEGRAM_TOKEN", "RW_TELEGRAM_TOKEN_FILE", environment, true); err != nil {
			return cfg, err
		}
		u, parseErr := url.Parse(cfg.WebhookPublicURL)
		if parseErr != nil || u.Scheme != "https" || u.Host == "" || cfg.WebhookSecretFile == "" {
			return cfg, errors.New("valid HTTPS webhook URL and secret file are required")
		}
	default:
		return cfg, errors.New("RW_TELEGRAM_MODE must be disabled, direct_polling, direct_webhook or natsproxy")
	}
	if cfg.TelegramMode != "disabled" && (environment == Staging || environment == Prod || cfg.TelegramMode != "direct_polling") && (cfg.TelegramBotID <= 0 || cfg.TelegramBotUsername == "") {
		return cfg, errors.New("telegram bot ID and username are required for this environment and mode")
	}
	return cfg, nil
}

func loadSessions() (SessionsConfig, error) {
	cfg := SessionsConfig{USDTNetwork: os.Getenv("RW_USDT_NETWORK")}
	var err error
	if cfg.PageSize, err = integer("RW_SESSION_LIST_PAGE_SIZE", 20); err != nil {
		return cfg, err
	}
	if cfg.USDTNetwork == "" {
		return cfg, errors.New("RW_USDT_NETWORK is required")
	}
	if cfg.PageSize < 1 || cfg.PageSize > 100 {
		return cfg, errors.New("RW_SESSION_LIST_PAGE_SIZE must be between 1 and 100")
	}
	if err = fixed("RW_SESSION_LIST_PAGE_MAX", "100"); err != nil {
		return cfg, err
	}
	if err = fixed("RW_VERIFICATION_DEADLINE_MAX", "2h"); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func rejectUnknown() error {
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(name, "RW_") {
			if _, ok := allowed[name]; !ok {
				return fmt.Errorf("unknown configuration variable %s", name)
			}
		}
	}
	return nil
}

func secretValue(valueName, fileName string, environment Environment, required bool) (string, error) {
	direct, path := strings.TrimSpace(os.Getenv(valueName)), strings.TrimSpace(os.Getenv(fileName))
	if direct != "" && path != "" {
		return "", fmt.Errorf("configure only one of %s and %s", valueName, fileName)
	}
	if (environment == Staging || environment == Prod) && direct != "" {
		return "", fmt.Errorf("%s is forbidden in staging/prod", valueName)
	}
	if path != "" {
		if err := validateFile(path); err != nil {
			return "", fmt.Errorf("validate %s: %w", fileName, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", fileName, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if required && direct == "" {
		return "", fmt.Errorf("%s or %s is required", valueName, fileName)
	}
	return direct, nil
}

func validateDatabasePolicy(raw string, environment Environment) error {
	if environment != Staging && environment != Prod {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		return errors.New("production database URL must use postgres:// or postgresql://")
	}
	if parsed.Query().Get("sslmode") != "verify-full" {
		return errors.New("production database URL must use sslmode=verify-full")
	}
	return nil
}

func validateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect secret file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("secret path must be a regular non-symlink file")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("secret file must not be group/world-writable")
	}
	return nil
}

func fixed(name, expected string) error {
	if actual := strings.TrimSpace(os.Getenv(name)); actual != "" && actual != expected {
		return fmt.Errorf("%s is fixed to %s", name, expected)
	}
	return nil
}

func value(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "localhost"
	}
	return h
}
func split(raw string) []string {
	var result []string
	for _, v := range strings.Split(raw, ",") {
		if v = strings.TrimSpace(v); v != "" {
			result = append(result, v)
		}
	}
	return result
}
func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := value(name, fallback.String())
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return v, nil
}
func integer(name string, fallback int) (int, error) {
	raw := value(name, strconv.Itoa(fallback))
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return v, nil
}
func integerRequired(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return v, nil
}
func int64Value(name string, fallback int64) (int64, error) {
	raw := value(name, strconv.FormatInt(fallback, 10))
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return v, nil
}
func int64Required(name string) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return v, nil
}
func int32Value(name string, fallback int32) (int32, error) {
	v, err := int64Value(name, int64(fallback))
	if err != nil || v < 0 || v > int64(^uint32(0)>>1) {
		return 0, fmt.Errorf("%s must be a valid non-negative int32", name)
	}
	return int32(v), nil
}
func bytesValue(name string, fallback int) (int, error) {
	raw := value(name, strconv.Itoa(fallback))
	multipliers := map[string]int{"KiB": 1 << 10, "MiB": 1 << 20}
	for suffix, multiplier := range multipliers {
		if strings.HasSuffix(raw, suffix) {
			n, err := strconv.Atoi(strings.TrimSuffix(raw, suffix))
			if err != nil || n < 0 {
				return 0, fmt.Errorf("%s must be bytes, KiB or MiB", name)
			}
			return n * multiplier, nil
		}
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%s must be bytes, KiB or MiB", name)
	}
	return v, nil
}
