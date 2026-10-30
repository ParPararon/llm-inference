package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"llm-inference/gateway/internal/edge"
	"llm-inference/gateway/internal/upstream"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)

	if err := runGatewayApplication(logger); err != nil {
		logger.WithError(err).Fatal("gateway stopped with an error")
	}
}

func runGatewayApplication(logger *logrus.Logger) error {
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load gateway configuration: %w", err)
	}

	handler, err := buildGatewayHandler(config, logger)
	if err != nil {
		return fmt.Errorf("build gateway handler: %w", err)
	}

	server := &http.Server{
		Addr:              config.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return runGatewayServer(ctx, server, logger)
}

func buildGatewayHandler(config Config, logger *logrus.Logger) (http.Handler, error) {
	authenticateFn, err := edge.NewAuthenticationMiddleware(config.APIKey)
	if err != nil {
		return nil, fmt.Errorf("initialize authentication middleware: %w", err)
	}

	proxy := upstream.NewReverseProxy(config.UpstreamURL, "")
	router := edge.NewInboundRouter(proxy, authenticateFn)

	return edge.NewRequestLoggingMiddleware(logger)(router), nil
}

func runGatewayServer(ctx context.Context, server *http.Server, logger *logrus.Logger) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP requests: %w", err)
	}
	defer listener.Close()

	group, groupCtx := errgroup.WithContext(ctx)

	logger.WithField("address", listener.Addr().String()).Info("gateway listening")

	group.Go(func() error {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve HTTP requests: %w", err)
	})

	group.Go(func() error {
		<-groupCtx.Done()
		if ctx.Err() == nil {
			return nil
		}

		logger.Info("gateway shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}

		return nil
	})

	if err := group.Wait(); err != nil {
		return err
	}

	logger.Info("gateway stopped")
	return nil
}
