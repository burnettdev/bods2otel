package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/burnettdev/bods2otel/pkg/logging"
	"github.com/burnettdev/bods2otel/pkg/otel/logs"
	"github.com/burnettdev/bods2otel/pkg/pipeline"
	"github.com/burnettdev/bods2otel/pkg/profiling"
	"github.com/burnettdev/bods2otel/pkg/tracing"
)

func main() {
	// Initialize logger first so LOG_LEVEL is available
	logging.Init()
	logger := logging.Get()

	logger.DebugCall("main")

	// Command line flags
	var (
		dryRun    = flag.Bool("dry-run", false, "Print data to stdout instead of sending to OTel")
		apiKey    = flag.String("api-key", getEnv("BODS_API_KEY", ""), "BODS API key (required)")
		datasetID = flag.String("dataset-id", getEnv("BODS_DATASET_ID", "699"), "BODS dataset ID")
		lineRefs  = flag.String("line-refs", getEnv("BODS_LINE_REFS", "49x"), "Bus line references, comma-separated")
		interval  = flag.String("interval", getEnv("BODS_INTERVAL", "30s"), "Polling interval")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "BODS to OTel Bus Tracking Pipeline\n\n")
		fmt.Fprintf(os.Stderr, "Fetches live bus tracking data from the UK Department for Transport's\n")
		fmt.Fprintf(os.Stderr, "Bus Open Data Service (BODS), converts XML to JSON, and sends it to\n")
		fmt.Fprintf(os.Stderr, "OpenTelemetry for log aggregation and analysis.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  BODS_API_KEY      - Your BODS API key (required)\n")
		fmt.Fprintf(os.Stderr, "  BODS_DATASET_ID   - BODS dataset ID (default: 699)\n")
		fmt.Fprintf(os.Stderr, "  BODS_LINE_REFS    - Bus line references, comma-separated (default: 49x)\n")
		fmt.Fprintf(os.Stderr, "  BODS_INTERVAL     - Polling interval (default: 30s)\n")
		fmt.Fprintf(os.Stderr, "\nOpenTelemetry Configuration:\n")
		fmt.Fprintf(os.Stderr, "  OTEL_LOGS_ENABLED              - Enable OTel logging (default: true)\n")
		fmt.Fprintf(os.Stderr, "  OTEL_EXPORTER_OTLP_ENDPOINT    - OTLP endpoint (default: localhost:4318)\n")
		fmt.Fprintf(os.Stderr, "  OTEL_EXPORTER_OTLP_LOGS_ENDPOINT - OTLP logs endpoint (overrides shared)\n")
		fmt.Fprintf(os.Stderr, "  OTEL_EXPORTER_OTLP_INSECURE    - Use insecure connection (default: true)\n")
		fmt.Fprintf(os.Stderr, "  OTEL_EXPORTER_OTLP_PROTOCOL    - Protocol: http or grpc (default: http)\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Dry run mode (safe for testing)\n")
		fmt.Fprintf(os.Stderr, "  %s --dry-run --api-key=YOUR_API_KEY --line-refs=49x\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Production mode with OTel\n")
		fmt.Fprintf(os.Stderr, "  %s --api-key=YOUR_API_KEY --line-refs=49x,7\n\n", os.Args[0])
	}

	flag.Parse()

	// Validate required parameters
	if *apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: API key is required. Use --api-key or set BODS_API_KEY environment variable.\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Parse interval
	intervalDuration, err := time.ParseDuration(*interval)
	if err != nil {
		logger.Error("Invalid interval format", "error", err, "interval", *interval)
		os.Exit(1)
	}

	// Parse line references
	lineRefsList := strings.Split(*lineRefs, ",")
	for i, ref := range lineRefsList {
		lineRefsList[i] = strings.TrimSpace(ref)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize tracing
	shutdownTracing, err := tracing.InitTracing()
	if err != nil {
		logger.Error("Failed to initialize OpenTelemetry tracing", "error", err)
		// Continue without tracing rather than failing
	}
	defer shutdownTracing()

	// Initialize OpenTelemetry logging
	shutdownLogs, err := logs.InitLogs()
	if err != nil {
		logger.Error("Failed to initialize OpenTelemetry logging", "error", err)
		// Continue without logging rather than failing
	}
	defer shutdownLogs()

	// Initialize profiling
	shutdownProfiling, err := profiling.InitProfiling()
	if err != nil {
		logger.Error("Failed to initialize profiling", "error", err)
		// Continue without profiling rather than failing
	}
	defer shutdownProfiling()

	// Create pipeline configuration
	config := pipeline.Config{
		DryRun:    *dryRun,
		APIKey:    *apiKey,
		DatasetID: *datasetID,
		LineRefs:  lineRefsList,
		Interval:  intervalDuration,
	}

	// Create pipeline
	pipelineInstance, err := pipeline.New(config)
	if err != nil {
		logger.Error("Failed to create pipeline", "error", err)
		os.Exit(1)
	}

	// Print startup information
	if *dryRun {
		logger.Info("Starting BODS to OTel pipeline in DRY RUN mode")
		logger.Info("Data will be printed to stdout, not sent to OTel")
	} else {
		logger.Info("Starting BODS to OTel pipeline in PRODUCTION mode")
		logger.Info("Data will be sent via OpenTelemetry")
	}
	logger.Info("Application started successfully",
		"lines", lineRefsList,
		"interval", intervalDuration,
	)

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start pipeline in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- pipelineInstance.Run(ctx)
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		logger.Info("Received shutdown signal", "signal", sig)
		logger.Debug("Graceful shutdown initiated")
		cancel()
		// Wait a bit for graceful shutdown
		select {
		case <-time.After(5 * time.Second):
			logger.Warn("Shutdown timeout, forcing exit")
		case <-errChan:
			logger.Debug("Pipeline stopped")
		}
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			logger.Error("Pipeline error", "error", err)
			os.Exit(1)
		}
		logger.Debug("Pipeline stopped")
	case <-ctx.Done():
		logger.Debug("Context cancelled")
	}
}

// getEnv returns the value of an environment variable or a default value if not set
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
