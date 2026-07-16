// Command card-backend runs the Vultisig Card backend service: the
// app-facing API, the issuer webhook sink, and the program monitors.
package main

import (
	"cmp"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultAddr       = ":8080"
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)

	srv := &http.Server{
		Addr:              cmp.Or(os.Getenv("CARD_BACKEND_ADDR"), defaultAddr),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
