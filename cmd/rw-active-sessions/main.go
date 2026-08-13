package main

import (
	"context"
	"os"

	"github.com/dmuraveiko/RW/internal/platform/runtime"
)

func main() {
	if err := runtime.Run(context.Background(), runtime.ActiveSessions); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
