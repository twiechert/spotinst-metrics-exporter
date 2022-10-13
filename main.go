package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bonial-International-GmbH/spotinst-metrics-exporter/pkg/collectors"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spotinst/spotinst-sdk-go/service/mcs"
	"github.com/spotinst/spotinst-sdk-go/service/ocean"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst/session"
	"go.uber.org/zap"
)

var logger logr.Logger

func init() {
	// Set up a production logger which will write JSON logs.
	zapLog, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup logger: %v", err)
		os.Exit(1)
	}

	logger = zapr.NewLogger(zapLog)
}

func main() {
	addr := flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	go handleSignals(cancel)

	sess := session.New()
	mcsClient := mcs.New(sess)

	oceanAWSClient := ocean.New(sess).CloudProviderAWS()

	clusters, err := getOceanAWSClusters(ctx, oceanAWSClient)
	if err != nil {
		logger.Error(err, "failed to fetch ocean clusters")
		os.Exit(1)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewOceanAWSClusterCostsCollector(ctx, logger, mcsClient, clusters))
	registry.MustRegister(collectors.NewOceanAWSResourceSuggestionsCollector(ctx, logger, oceanAWSClient, clusters))

	handler := http.NewServeMux()
	handler.HandleFunc("/healthz", healthzHandler)
	handler.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true}))

	listenAndServe(ctx, handler, *addr)
}

func handleSignals(cancelFunc func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	<-signals
	logger.Info("received signal, terminating...")
	cancelFunc()
}

func listenAndServe(ctx context.Context, handler http.Handler, addr string) {
	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	go func() {
		logger.Info("starting server", "addr", addr)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err, "failed to start server")
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	logger.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error(err, "failed to shutdown HTTP server")
		os.Exit(1)
	}
}

func getOceanAWSClusters(ctx context.Context, client aws.Service) ([]*aws.Cluster, error) {
	output, err := client.ListClusters(ctx, &aws.ListClustersInput{})
	if err != nil {
		return nil, err
	}

	return output.Clusters, nil
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := w.Write([]byte("ok")); err != nil {
		logger.Error(err, "failed to write health check status")
	}
}
