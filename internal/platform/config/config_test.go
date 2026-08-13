package config

import "testing"

func TestLoadBotNATSProxy(t *testing.T) {
	baseEnvironment(t)
	t.Setenv("RW_TELEGRAM_MODE", "natsproxy")
	t.Setenv("RW_TELEGRAM_BOT_ID", "123")
	t.Setenv("RW_TELEGRAM_BOT_USERNAME", "realwallet_test_bot")
	t.Setenv("RW_USDT_NETWORK", "fixture-net")
	t.Setenv("RW_USDT_SCALE", "6")
	t.Setenv("RW_ACTIVATION_AMOUNT_MIN_MINOR", "1000")
	t.Setenv("RW_ACTIVATION_AMOUNT_MAX_MINOR", "2999")
	cfg, err := Load(Bot)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.TelegramMode != "natsproxy" || cfg.Database.PoolMax != 20 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsUnknownProjectVariable(t *testing.T) {
	baseEnvironment(t)
	t.Setenv("RW_UNKNOWN_OPTION", "value")
	if _, err := Load(ActiveSessions); err == nil {
		t.Fatal("unknown RW_ variable must fail")
	}
}

func TestLoadLocalDisabledBot(t *testing.T) {
	baseEnvironment(t)
	t.Setenv("RW_TELEGRAM_MODE", "disabled")
	t.Setenv("RW_TELEGRAM_BOT_ID", "")
	t.Setenv("RW_TELEGRAM_BOT_USERNAME", "")
	t.Setenv("RW_USDT_NETWORK", "fixture-net")
	t.Setenv("RW_USDT_SCALE", "6")
	t.Setenv("RW_ACTIVATION_AMOUNT_MIN_MINOR", "1000")
	t.Setenv("RW_ACTIVATION_AMOUNT_MAX_MINOR", "2999")
	if _, err := Load(Bot); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsSmallAmountRange(t *testing.T) {
	baseEnvironment(t)
	t.Setenv("RW_TELEGRAM_MODE", "natsproxy")
	t.Setenv("RW_TELEGRAM_BOT_ID", "123")
	t.Setenv("RW_TELEGRAM_BOT_USERNAME", "realwallet_test_bot")
	t.Setenv("RW_USDT_NETWORK", "fixture-net")
	t.Setenv("RW_USDT_SCALE", "6")
	t.Setenv("RW_ACTIVATION_AMOUNT_MIN_MINOR", "1000")
	t.Setenv("RW_ACTIVATION_AMOUNT_MAX_MINOR", "1001")
	if _, err := Load(Bot); err == nil {
		t.Fatal("small activation range must fail")
	}
}

func TestLoadLocalPollingDiscoversBotIdentity(t *testing.T) {
	baseEnvironment(t)
	t.Setenv("RW_TELEGRAM_MODE", "direct_polling")
	t.Setenv("RW_TELEGRAM_BOT_ID", "")
	t.Setenv("RW_TELEGRAM_BOT_USERNAME", "")
	t.Setenv("RW_TELEGRAM_TOKEN", "123456789:abcdefghijklmnopqrstuvwxyz_ABC")
	t.Setenv("RW_USDT_NETWORK", "fixture-net")
	t.Setenv("RW_USDT_SCALE", "6")
	t.Setenv("RW_ACTIVATION_AMOUNT_MIN_MINOR", "1000")
	t.Setenv("RW_ACTIVATION_AMOUNT_MAX_MINOR", "2999")
	cfg, err := Load(Bot)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.TelegramToken == "" || cfg.Bot.TelegramBotID != 0 || cfg.Bot.TelegramBotUsername != "" {
		t.Fatalf("unexpected bot config: %+v", cfg.Bot)
	}
}

func baseEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("RW_ENVIRONMENT", "test")
	t.Setenv("RW_DATABASE_URL", "postgres://user:password@localhost:5432/database?sslmode=disable")
	t.Setenv("RW_NATS_URLS", "nats://localhost:4222")
	t.Setenv("RW_SIGNING_KEY_ID", "fixture-test")
	t.Setenv("RW_SIGNING_PRIVATE_KEY_BASE64", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("RW_TRUSTED_KEYS_FILE", "/tmp/trusted.json")
	t.Setenv("RW_DATA_KEYRING_FILE", "/tmp/keyring.json")
	t.Setenv("RW_FINGERPRINT_KEY_FILE", "/tmp/fingerprint.key")
}
