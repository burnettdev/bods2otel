package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"bods2otel/pkg/types"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	username   string
	password   string
	tracer     trace.Tracer
}

type PushRequest struct {
	Streams []Stream `json:"streams"`
}

type Stream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

func NewClient(baseURL, username, password string) *Client {
	// Create HTTP client with OpenTelemetry instrumentation
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   30 * time.Second,
	}

	return &Client{
		httpClient: client,
		baseURL:    baseURL,
		username:   username,
		password:   password,
		tracer:     otel.Tracer("loki-client"),
	}
}

func (c *Client) SendBusData(ctx context.Context, data *types.ParsedBusData) error {
	ctx, span := c.tracer.Start(ctx, "loki.send_bus_data",
		trace.WithAttributes(
			attribute.String("line_ref", data.LineRef),
			attribute.Int("vehicles_count", len(data.VehicleData)),
		),
	)
	defer span.End()

	// Create individual log entries for each vehicle
	var logValues [][]string

	for _, vehicle := range data.VehicleData {
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
			"bus_image":                      vehicle.BusImage,
		}

		// Convert vehicle to JSON
		vehicleJSON, err := json.Marshal(vehicleLog)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to marshal vehicle JSON: %w", err)
		}

		// Add to log values with current timestamp
		logValues = append(logValues, []string{
			strconv.FormatInt(time.Now().UnixNano(), 10),
			string(vehicleJSON),
		})
	}

	// Create Loki push request with individual log lines
	lokiReq := PushRequest{
		Streams: []Stream{
			{
				Stream: map[string]string{
					"job":      "bods2otel",
					"service":  "bus-tracking",
					"line_ref": data.LineRef,
				},
				Values: logValues,
			},
		},
	}

	// Marshal Loki request
	reqBody, err := json.Marshal(lokiReq)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal Loki request: %w", err)
	}

	// Send to Loki
	url := fmt.Sprintf("%s/loki/api/v1/push", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bods2otel/1.0.0")

	// Add basic authentication if credentials are provided
	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
		span.SetAttributes(
			attribute.Bool("auth.enabled", true),
			attribute.String("auth.username", c.username),
		)
	} else {
		span.SetAttributes(
			attribute.Bool("auth.enabled", false),
		)
	}

	span.SetAttributes(
		attribute.String("http.url", url),
		attribute.String("http.method", "POST"),
		attribute.Int("request.size_bytes", len(reqBody)),
		attribute.Int("log_lines_count", len(logValues)),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	span.SetAttributes(
		attribute.Int("http.status_code", resp.StatusCode),
	)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("Loki returned status %d", resp.StatusCode)
		span.RecordError(err)
		return err
	}

	return nil
}
