package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmuraveiko/RW/internal/platform/migrate"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	service := flag.String("service", "", "database to migrate: bot or sessions")
	root := flag.String("root", "migrations", "migration directory")
	flag.Parse()
	if *service != "bot" && *service != "sessions" {
		fail(migrate.ErrInvalidService)
	}
	databaseURL, err := databaseURL()
	if err != nil {
		fail(err)
	}
	if err = migrate.Up(context.Background(), databaseURL, filepath.Join(*root, *service)); err != nil {
		fail(err)
	}
}

func databaseURL() (string, error) {
	direct, path := strings.TrimSpace(os.Getenv("RW_DATABASE_URL")), strings.TrimSpace(os.Getenv("RW_DATABASE_URL_FILE"))
	environment := strings.TrimSpace(os.Getenv("RW_ENVIRONMENT"))
	if direct != "" && path != "" {
		return "", fmt.Errorf("configure only one database URL source")
	}
	if (environment == "staging" || environment == "prod") && direct != "" {
		return "", fmt.Errorf("RW_DATABASE_URL is forbidden in staging/prod")
	}
	if path != "" {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			return "", fmt.Errorf("database URL file must be a protected regular file")
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read database URL: %w", err)
		}
		return strings.TrimSpace(string(contents)), nil
	}
	if environment == "staging" || environment == "prod" {
		return "", fmt.Errorf("RW_DATABASE_URL_FILE is required in staging/prod")
	}
	if direct == "" {
		return "", fmt.Errorf("RW_DATABASE_URL or RW_DATABASE_URL_FILE is required")
	}
	return direct, nil
}

func fail(err error) { _, _ = fmt.Fprintln(os.Stderr, err); os.Exit(1) }
