package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nczz/kiro-discord-bot/internal/websharerelay"
)

var version = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "serve":
		if err := serve(); err != nil {
			slog.Error("webshare relay serve failed", "err", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println(version)
	case "healthcheck":
		if err := healthcheck(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func serve() error {
	cfg, err := websharerelay.ConfigFromEnv()
	if err != nil {
		return err
	}
	if cfg.HostToken == "" {
		return errors.New("RELAY_HOST_TOKEN or RELAY_HOST_TOKEN_FILE is required for host WebSocket authentication")
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel(cfg.LogLevel)}))

	relay, err := websharerelay.NewServer(cfg, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.MetricsAddr != "" {
		metricsServer := &http.Server{Addr: cfg.MetricsAddr, Handler: relay.MetricsHandler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			logger.Info("webshare relay metrics listening", "addr", cfg.MetricsAddr)
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("webshare relay metrics failed", "err", err)
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = metricsServer.Shutdown(shutdownCtx)
		}()
	}

	server := &http.Server{Addr: cfg.Addr, Handler: relay, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("webshare relay listening", "addr", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func healthcheck(args []string) error {
	url := ""
	if len(args) > 0 {
		url = args[0]
	}
	if url == "" {
		url = strings.TrimRight(os.Getenv("RELAY_PUBLIC_BASE_URL"), "/")
	}
	if url == "" {
		url = "http://127.0.0.1" + websharerelay.DefaultAddr
	}
	url = strings.TrimRight(url, "/") + "/healthz"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck failed: %s", resp.Status)
	}
	return nil
}

func slogLevel(level string) slog.Leveler {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: webshare-relay [serve|version|healthcheck]")
}
