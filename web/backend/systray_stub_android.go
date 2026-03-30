//go:build android

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sipeed/picoclaw/pkg/logger"
)

// runTray falls back to headless mode on Android (no system tray available).
func runTray() {
	logger.Info("System tray is unavailable on Android; running without tray")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	shutdownApp()
}
