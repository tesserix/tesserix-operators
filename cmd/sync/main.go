package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/tesserix/devai-sandbox-operator/internal/sandboxsync"
)

func main() {
	var policyPath string
	flag.StringVar(&policyPath, "policy", "", "path to the sync policy")
	flag.Parse()
	if err := sandboxsync.Run(context.Background(), sandboxsync.RunConfig{PolicyPath: policyPath, SourceURL: os.Getenv("SOURCE_DATABASE_URL"), TargetURL: os.Getenv("TARGET_DATABASE_URL"), Salt: os.Getenv("ANONYMIZATION_SALT")}); err != nil {
		slog.Error("sandbox data sync failed", "error", err)
		os.Exit(1)
	}
	slog.Info("sandbox data sync completed")
}
