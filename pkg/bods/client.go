package bods

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	BaseURLTemplate = "https://data.bus-data.dft.gov.uk/api/v1/datafeed/%s/"
)

type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	tracer     trace.Tracer
}

type BusData struct {
	XMLData   string
	Timestamp time.Time
	LineRef   string
}

func NewClient(apiKey, datasetID string) *Client {
	// Create HTTP client with OpenTelemetry instrumentation
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   30 * time.Second,
	}

	baseURL := fmt.Sprintf(BaseURLTemplate, datasetID)

	return &Client{
		httpClient: client,
		apiKey:     apiKey,
		baseURL:    baseURL,
		tracer:     otel.Tracer("bods-client"),
	}
}

func (c *Client) FetchBusData(ctx context.Context, lineRef string) (*BusData, error) {
	ctx, span := c.tracer.Start(ctx, "bods.fetch_bus_data",
		trace.WithAttributes(
			attribute.String("line_ref", lineRef),
			attribute.String("api.endpoint", c.baseURL),
		),
	)
	defer span.End()

	// Build URL with parameters
	url := fmt.Sprintf("%s?api_key=%s&lineRef=%s", c.baseURL, c.apiKey, lineRef)

	span.SetAttributes(
		attribute.String("http.url", url),
		attribute.String("http.method", "GET"),
	)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "bods2otel/1.0.0")
	req.Header.Set("Accept", "*/*")

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	span.SetAttributes(
		attribute.Int("http.status_code", resp.StatusCode),
		attribute.String("http.response.content_type", resp.Header.Get("Content-Type")),
	)

	if resp.StatusCode != http.StatusOK {
		// Read the error response body for debugging
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		span.RecordError(err)
		return nil, err
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	span.SetAttributes(
		attribute.Int("response.size_bytes", len(body)),
	)

	return &BusData{
		XMLData:   string(body),
		Timestamp: time.Now(),
		LineRef:   lineRef,
	}, nil
}
