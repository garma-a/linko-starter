package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

func initializeLogger() (*slog.Logger, func(), error) {
	logFile := os.Getenv("LINKO_LOG_FILE")
	stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	if logFile == "" {
		return slog.New(stderrHandler), func() {}, nil
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return nil, nil, err
	}
	buff := bufio.NewWriterSize(f, 8192)
	fileHandler := slog.NewJSONHandler(buff, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	multiHandler := slog.NewMultiHandler(stderrHandler, fileHandler)
	return slog.New(multiHandler), func() {
		buff.Flush()
		f.Close()
	}, nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	logger, cleanup, err := initializeLogger()
	if err != nil {
		return 1
	}
	defer cleanup()

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to create store: %v", err))
		return 1
	}

	logger.Debug(fmt.Sprintf("Linko is running on http://localhost:%d", httpPort))
	s := newServer(*st, logger, httpPort, cancel)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	logger.Debug("Linko is shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error(fmt.Sprintf("failed to shutdown server: %v", err))
		return 1
	}
	if serverErr != nil {
		logger.Error(fmt.Sprintf("server error: %v", serverErr))
		return 1
	}
	return 0
}
