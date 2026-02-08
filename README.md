# BODS2Otel

🚌 A Go service that fetches live bus tracking data from the UK Department for Transport's Bus Open Data Service (BODS) and streams it via OpenTelemetry (OTLP) for real-time monitoring and analysis.

## Prerequisites

- BODS API key (get from [data.bus-data.dft.gov.uk](https://data.bus-data.dft.gov.uk))
- OpenTelemetry-compatible backend

## Demo

A live demo showing some Bristol bus routes can be found [here.](https://elfordo.grafana.net/public-dashboards/2c542d2643314f34b82fcf7942787443)

## Configuration

Create a `.env` file in the project root with the following variables:

```env
BODS_API_KEY=your_bods_api_key_here
BODS_DATASET_ID=699
BODS_LINE_REFS=49x,7
BODS_INTERVAL=30s

# Shared OpenTelemetry Configuration (applies to both logs and traces)
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http
OTEL_EXPORTER_OTLP_INSECURE=true
OTEL_EXPORTER_OTLP_HEADERS=

# OpenTelemetry Logs Configuration
OTEL_LOGS_ENABLED=true

# OpenTelemetry Tracing Configuration (Optional)
OTEL_TRACING_ENABLED=false

# Application Logging Configuration
LOG_LEVEL=info

# Pyroscope Profiling Configuration (Optional)
PYROSCOPE_PROFILING_ENABLED=false
PYROSCOPE_SERVER_ADDRESS=http://localhost:4040
PYROSCOPE_APPLICATION_NAME=bods2otel
```

### OpenTelemetry Configuration

The application uses OpenTelemetry Protocol (OTLP) to send logs and traces to any compatible backend. It follows the OpenTelemetry specification by using **shared environment variables** for common settings, with signal-specific overrides when needed.

#### Shared Environment Variables (Recommended)

These apply to both logs and traces:

- `OTEL_EXPORTER_OTLP_ENDPOINT`: OTLP endpoint (default: `localhost:4318`)
- `OTEL_EXPORTER_OTLP_PROTOCOL`: Protocol to use - `http` or `grpc` (default: `http`)
- `OTEL_EXPORTER_OTLP_INSECURE`: Set to `true` for insecure connections (default: `true`)
- `OTEL_EXPORTER_OTLP_HEADERS`: Headers for export (format: `key1=value1,key2=value2`)

#### Signal-Specific Overrides

You can override shared settings for specific signals if needed:

**For Logs:**
- `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`: Override endpoint for logs only
- `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL`: Override protocol for logs only
- `OTEL_EXPORTER_OTLP_LOGS_INSECURE`: Override insecure setting for logs only
- `OTEL_EXPORTER_OTLP_LOGS_HEADERS`: Override headers for logs only

**For Traces:**
- `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`: Override endpoint for traces only
- `OTEL_EXPORTER_OTLP_TRACES_INSECURE`: Override insecure setting for traces only
- `OTEL_EXPORTER_OTLP_TRACES_HEADERS`: Override headers for traces only

#### Protocol Support
- **HTTP/Protobuf** (default): Use `OTEL_EXPORTER_OTLP_PROTOCOL=http`
- **gRPC**: Use `OTEL_EXPORTER_OTLP_PROTOCOL=grpc`

#### Authentication

For authenticated backends, use the shared `OTEL_EXPORTER_OTLP_HEADERS` environment variable:

```env
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer token,api-key=key
```

### Logging Configuration

The application uses structured logging with configurable log levels.

#### Log Levels

Set the `LOG_LEVEL` environment variable to control logging verbosity:

- `debug`: Shows all logs including debug calls, environment variables, and HTTP requests
- `info`: Shows info, warn, and error logs (default)
- `warn`: Shows only warning and error logs
- `error`: Shows only error logs

### OpenTelemetry Tracing Configuration

The application supports distributed tracing using OpenTelemetry. This is optional and disabled by default.

#### Environment Variables

- `OTEL_TRACING_ENABLED`: Set to `true` or `1` to enable tracing

Tracing uses the shared `OTEL_EXPORTER_OTLP_*` environment variables (see above). You can override with `OTEL_EXPORTER_OTLP_TRACES_*` variables if needed.

#### Trace Information

When tracing is enabled, the application will create spans for:

- **Main processing cycle**: Overall operation span (`pipeline.process_once`)
- **HTTP data fetch**: Fetching bus data from BODS API (`bods.fetch_bus_data`)
- **XML parsing**: Parsing the bus data (`xml-parser.parse_bus_data`)
- **Log emission**: Emitting OpenTelemetry log records (`pipeline.send_to_otel`)

Each span includes relevant attributes like HTTP status codes, durations, vehicle counts, and error information. Logs are automatically correlated with traces when both are enabled.

### Pyroscope Profiling Configuration

The application supports continuous profiling using Pyroscope. This is optional and disabled by default.

#### Environment Variables

- `PYROSCOPE_PROFILING_ENABLED`: Set to `true` or `1` to enable profiling
- `PYROSCOPE_SERVER_ADDRESS`: Pyroscope server address (default: `http://localhost:4040`)
- `PYROSCOPE_APPLICATION_NAME`: Application name for profiling (default: `bods2otel`)
- `PYROSCOPE_BASIC_AUTH_USER`: Basic auth username
- `PYROSCOPE_BASIC_AUTH_PASSWORD`: Basic auth password

## Installation

### Building from Source

1. Clone the repository:
```bash
git clone https://github.com/burnettdev/bods2otel.git
cd bods2otel
```

2. Install dependencies:
```bash
go mod tidy
```

3. Build the application:
```bash
go build -o bods2otel
```

4. Run the application:
```bash
./bods2otel
```

### Docker

Build and run with Docker:

```bash
# Build the image
docker build -t bods2otel .

# Run the container
docker run -d \
  --name bods2otel \
  -e BODS_API_KEY=your_bods_api_key_here \
  -e BODS_LINE_REFS=49x,7 \
  -e OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318 \
  -e OTEL_EXPORTER_OTLP_PROTOCOL=http \
  -e OTEL_TRACING_ENABLED=true \
  --restart unless-stopped \
  bods2otel
```

Pull from Github Container Registry:

```bash
# Run the container
docker run -d \
  --name bods2otel \
  -e BODS_API_KEY=your_bods_api_key_here \
  -e BODS_LINE_REFS=49x,7 \
  -e OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318 \
  -e OTEL_EXPORTER_OTLP_PROTOCOL=http \
  -e OTEL_TRACING_ENABLED=true \
  --restart unless-stopped \
  ghcr.io/burnettdev/bods2otel:latest
```

## Usage

The service will:
- Fetch bus data every 30 seconds (configurable via `BODS_INTERVAL`)
- Emit OpenTelemetry log records for each vehicle
- Automatically correlate logs with traces (if tracing is enabled)
- Log any errors that occur during the process

### Command Line Options

- `--dry-run`: Print data to stdout instead of sending to OTel
- `--api-key`: BODS API key (required)
- `--dataset-id`: BODS dataset ID (default: "699")
- `--line-refs`: Bus line references, comma-separated (default: "49x")
- `--interval`: Polling interval (default: "30s")

## Data Structure

Each vehicle entry is sent as an OpenTelemetry log record with:
- **Timestamp**: When the vehicle data was captured
- **Body**: Full vehicle data as JSON
- **Attributes**: Structured metadata including:
  - `service`: "bus-tracking"
  - `line_ref`: Bus line reference (e.g., "49x")
  - `vehicle_ref`: Vehicle reference identifier
  - `direction_ref`: Direction reference
  - `operator_ref`: Operator reference
  - `origin_name`: Origin name (if available)
  - `destination_name`: Destination name (if available)
  - `latitude`: Latitude (if available)
  - `longitude`: Longitude (if available)
  - `origin_aimed_departure_time`: Scheduled departure time (if available)
  - `destination_aimed_arrival_time`: Scheduled arrival time (if available)
  - `recorded_at_time`: When the data was recorded
  - `valid_until_time`: When the data expires

## Contributing

Feel free to open issues or submit pull requests!

## License

MIT License - feel free to use this project for whatever you'd like! Happy Bus Tracking

## Migration from Loki

If you were previously using the direct Loki integration, the service now uses OpenTelemetry Protocol (OTLP) instead. To route logs to Loki, use an OpenTelemetry Collector with a Loki exporter, or use Grafana Cloud which accepts OTLP directly.

<img width="auth" height="150" alt="image" src="https://github.com/user-attachments/assets/cbb403e2-7042-42af-8124-422128e146e8" />

BODS2Otel is proudly part of Snyk's Open Source Programme, ensuring vulnerabilities are found sooner rather than later. You can find more information <a href="https://snyk.io/?utm_source=open-source&utm_medium=pg-ptr&utm_campaign=ref-2501-osp&utm_content=pg-cta">here.</a>
