package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/burnettdev/bods2otel/pkg/bods"
	"github.com/burnettdev/bods2otel/pkg/logging"
	"github.com/burnettdev/bods2otel/pkg/otel/logs"
	"github.com/burnettdev/bods2otel/pkg/parser"
	"github.com/burnettdev/bods2otel/pkg/types"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

type Pipeline struct {
	config     Config
	bodsClient *bods.Client
	parser     *parser.XMLParser
	tracer     trace.Tracer
	logger     otellog.Logger
}

type Config struct {
	DryRun   bool
	APIKey   string
	DatasetID string
	LineRefs  []string
	Interval  time.Duration
}

func New(config Config) (*Pipeline, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if len(config.LineRefs) == 0 {
		return nil, fmt.Errorf("at least one line reference is required")
	}

	pipeline := &Pipeline{
		config:     config,
		bodsClient: bods.NewClient(config.APIKey, config.DatasetID),
		parser:     parser.NewXMLParser(),
		tracer:     otel.Tracer("pipeline"),
	}

	// Get OTel logger if not in dry run mode
	if !config.DryRun {
		pipeline.logger = logs.GetLogger("pipeline")
	}

	return pipeline, nil
}

func (p *Pipeline) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.config.Interval)
	defer ticker.Stop()

	logging.InfoCtx(ctx, "Pipeline started", "interval", p.config.Interval)

	// Process immediately on start
	if err := p.processOnce(ctx); err != nil {
		logging.ErrorCtx(ctx, "Error in initial processing", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			logging.DebugCtx(ctx, "Pipeline stopped")
			return ctx.Err()
		case <-ticker.C:
			logging.DebugCtx(ctx, "Ticker fired - processing data")
			if err := p.processOnce(ctx); err != nil {
				logging.ErrorCtx(ctx, "Error processing", "error", err)
			} else {
				logging.DebugCtx(ctx, "Data processing completed successfully")
			}
		}
	}
}

func (p *Pipeline) processOnce(ctx context.Context) error {
	ctx, span := p.tracer.Start(ctx, "pipeline.process_once",
		trace.WithAttributes(
			attribute.StringSlice("line_refs", p.config.LineRefs),
			attribute.Bool("dry_run", p.config.DryRun),
			attribute.Int("lines_count", len(p.config.LineRefs)),
		),
	)
	defer span.End()

	start := time.Now()

	// Process all lines concurrently
	type lineResult struct {
		lineRef string
		data    *types.ParsedBusData
		err     error
	}

	results := make(chan lineResult, len(p.config.LineRefs))

	// Start concurrent fetching for each line
	for _, lineRef := range p.config.LineRefs {
		go func(line string) {
			lineCtx, lineSpan := p.tracer.Start(ctx, "pipeline.process_line",
				trace.WithAttributes(attribute.String("line_ref", line)),
			)
			defer lineSpan.End()

			// Fetch data from BODS API
			busData, err := p.bodsClient.FetchBusData(lineCtx, line)
			if err != nil {
				lineSpan.RecordError(err)
				results <- lineResult{lineRef: line, err: fmt.Errorf("failed to fetch bus data for line %s: %w", line, err)}
				return
			}

			// Parse XML to JSON
			parsedData, err := p.parser.ParseBusData(lineCtx, busData)
			if err != nil {
				lineSpan.RecordError(err)
				results <- lineResult{lineRef: line, err: fmt.Errorf("failed to parse bus data for line %s: %w", line, err)}
				return
			}

			lineSpan.SetAttributes(
				attribute.Int("vehicles_processed", len(parsedData.VehicleData)),
			)

			results <- lineResult{lineRef: line, data: parsedData, err: nil}
		}(lineRef)
	}

	// Collect results
	var allData []*types.ParsedBusData
	var errors []error
	totalVehicles := 0

	for i := 0; i < len(p.config.LineRefs); i++ {
		result := <-results
		if result.err != nil {
			errors = append(errors, result.err)
			logging.ErrorCtx(ctx, "Error processing line", "line_ref", result.lineRef, "error", result.err)
		} else {
			allData = append(allData, result.data)
			totalVehicles += len(result.data.VehicleData)
		}
	}

	span.SetAttributes(
		attribute.Int("total_vehicles_processed", totalVehicles),
		attribute.Int("successful_lines", len(allData)),
		attribute.Int("failed_lines", len(errors)),
		attribute.String("processing_duration", time.Since(start).String()),
	)

	// Process successful results
	for _, data := range allData {
		if p.config.DryRun {
			if err := p.handleDryRun(ctx, data); err != nil {
				logging.ErrorCtx(ctx, "Error in dry run", "line_ref", data.LineRef, "error", err)
			}
		} else {
			if err := p.sendToOTel(ctx, data); err != nil {
				logging.ErrorCtx(ctx, "Error sending to OTel", "line_ref", data.LineRef, "error", err)
			}
		}
	}

	// Return error only if all lines failed
	if len(errors) == len(p.config.LineRefs) {
		return fmt.Errorf("all lines failed: %v", errors)
	}

	return nil
}

func (p *Pipeline) handleDryRun(ctx context.Context, data *types.ParsedBusData) error {
	_, span := p.tracer.Start(ctx, "pipeline.dry_run")
	defer span.End()

	// Print summary information
	fmt.Printf("\n=== DRY RUN - Bus Data for Line %s ===\n", data.LineRef)
	fmt.Printf("Timestamp: %s\n", data.Timestamp)
	fmt.Printf("Vehicles Found: %d\n", len(data.VehicleData))

	if len(data.VehicleData) > 0 {
		fmt.Println("\nVehicle Summary:")
		for i, vehicle := range data.VehicleData {
			route := ""
			if vehicle.OriginName != "" && vehicle.DestinationName != "" {
				route = fmt.Sprintf(" (%s → %s)", vehicle.OriginName, vehicle.DestinationName)
			}
			fmt.Printf("  %d. Vehicle: %s, Direction: %s, Location: (%.6f, %.6f)%s\n",
				i+1, vehicle.VehicleRef, vehicle.DirectionRef, vehicle.Latitude, vehicle.Longitude, route)
		}
	}

	fmt.Println("\nIndividual Log Lines (as sent to OTel):")
	fmt.Println("----------------------------------------")

	// Show individual log lines as they would be sent to OTel
	for i, vehicle := range data.VehicleData {
		// Create individual vehicle log entry
		vehicleLog := map[string]interface{}{
			"timestamp":                      data.Timestamp,
			"line_ref":                       data.LineRef,
			"vehicle_ref":                    vehicle.VehicleRef,
			"direction_ref":                  vehicle.DirectionRef,
			"operator_ref":                   vehicle.OperatorRef,
			"origin_ref":                     vehicle.OriginRef,
			"origin_name":                    vehicle.OriginName,
			"destination_ref":                vehicle.DestinationRef,
			"destination_name":               vehicle.DestinationName,
			"origin_aimed_departure_time":    vehicle.OriginAimedDepartureTime,
			"destination_aimed_arrival_time": vehicle.DestinationAimedArrivalTime,
			"longitude":                      vehicle.Longitude,
			"latitude":                       vehicle.Latitude,
			"recorded_at_time":               vehicle.RecordedAtTime,
			"valid_until_time":               vehicle.ValidUntilTime,
		}

		// Convert vehicle to JSON
		vehicleJSON, err := json.Marshal(vehicleLog)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to marshal vehicle JSON for dry run: %w", err)
		}

		fmt.Printf("Log Line %d: %s\n", i+1, string(vehicleJSON))
	}

	fmt.Println("=== END DRY RUN ===\n")

	span.SetAttributes(
		attribute.Int("vehicles_printed", len(data.VehicleData)),
	)

	return nil
}

func (p *Pipeline) sendToOTel(ctx context.Context, data *types.ParsedBusData) error {
	ctx, span := p.tracer.Start(ctx, "pipeline.send_to_otel")
	defer span.End()

	if p.logger == nil {
		logging.WarnCtx(ctx, "OpenTelemetry logger not initialized, skipping log emission")
		return nil
	}

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, data.Timestamp)
	if err != nil {
		// Fallback to current time if parsing fails
		timestamp = time.Now()
	}

	logsEmitted := 0

	// Emit log records for each vehicle
	for _, vehicle := range data.VehicleData {
		// Marshal vehicle to JSON for the log body
		vehicleJSON, err := json.Marshal(vehicle)
		if err != nil {
			span.RecordError(err)
			logging.ErrorCtx(ctx, "Failed to marshal vehicle data", "error", err)
			continue
		}

		// Build attributes for the log record
		attrs := []otellog.KeyValue{
			otellog.String("service", "bus-tracking"),
			otellog.String("line_ref", data.LineRef),
			otellog.String("vehicle_ref", vehicle.VehicleRef),
			otellog.String("direction_ref", vehicle.DirectionRef),
			otellog.String("operator_ref", vehicle.OperatorRef),
		}

		// Add optional fields as attributes
		if vehicle.OriginRef != "" {
			attrs = append(attrs, otellog.String("origin_ref", vehicle.OriginRef))
		}
		if vehicle.OriginName != "" {
			attrs = append(attrs, otellog.String("origin_name", vehicle.OriginName))
		}
		if vehicle.DestinationRef != "" {
			attrs = append(attrs, otellog.String("destination_ref", vehicle.DestinationRef))
		}
		if vehicle.DestinationName != "" {
			attrs = append(attrs, otellog.String("destination_name", vehicle.DestinationName))
		}
		if vehicle.Latitude != 0 {
			attrs = append(attrs, otellog.Float64("latitude", vehicle.Latitude))
		}
		if vehicle.Longitude != 0 {
			attrs = append(attrs, otellog.Float64("longitude", vehicle.Longitude))
		}
		if vehicle.OriginAimedDepartureTime != "" {
			attrs = append(attrs, otellog.String("origin_aimed_departure_time", vehicle.OriginAimedDepartureTime))
		}
		if vehicle.DestinationAimedArrivalTime != "" {
			attrs = append(attrs, otellog.String("destination_aimed_arrival_time", vehicle.DestinationAimedArrivalTime))
		}
		if vehicle.RecordedAtTime != "" {
			attrs = append(attrs, otellog.String("recorded_at_time", vehicle.RecordedAtTime))
		}
		if vehicle.ValidUntilTime != "" {
			attrs = append(attrs, otellog.String("valid_until_time", vehicle.ValidUntilTime))
		}

		// Create log record with trace context
		record := otellog.Record{}
		record.SetTimestamp(timestamp)
		record.SetSeverity(otellog.SeverityInfo)
		record.SetBody(otellog.StringValue(string(vehicleJSON)))

		// Add attributes to the record
		record.AddAttributes(attrs...)

		// Emit log record
		p.logger.Emit(ctx, record)

		logsEmitted++
	}

	logging.InfoCtx(ctx, "Successfully emitted vehicle log records to OTel",
		"logs_emitted", logsEmitted,
		"line_ref", data.LineRef,
	)

	span.SetAttributes(
		attribute.Int("vehicles_sent", logsEmitted),
		attribute.Int("otel.logs_emitted", logsEmitted),
	)

	return nil
}
