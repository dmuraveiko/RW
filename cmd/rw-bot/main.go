package main

import (
	"context"
	"os"

	"github.com/dmuraveiko/RW/internal/platform/runtime"
)

func main() {
	if err := runtime.Run(context.Background(), runtime.Bot); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
