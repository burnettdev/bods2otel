package parser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/burnettdev/bods2otel/pkg/bods"
	"github.com/burnettdev/bods2otel/pkg/types"

	"github.com/clbanning/mxj/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type XMLParser struct {
	tracer trace.Tracer
}

func NewXMLParser() *XMLParser {
	return &XMLParser{
		tracer: otel.Tracer("xml-parser"),
	}
}

func (p *XMLParser) ParseBusData(ctx context.Context, busData *bods.BusData) (*types.ParsedBusData, error) {
	ctx, span := p.tracer.Start(ctx, "xml_parser.parse_bus_data",
		trace.WithAttributes(
			attribute.String("line_ref", busData.LineRef),
			attribute.Int("xml_size_bytes", len(busData.XMLData)),
		),
	)
	defer span.End()

	// Parse XML to map
	xmlMap, err := mxj.NewMapXml([]byte(busData.XMLData))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	// Extract vehicle activities
	vehicles, err := p.extractVehicleActivities(ctx, xmlMap)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to extract vehicle activities: %w", err)
	}

	span.SetAttributes(
		attribute.Int("vehicles_count", len(vehicles)),
	)

	return &types.ParsedBusData{
		LineRef:     busData.LineRef,
		Timestamp:   busData.Timestamp.Format("2006-01-02T15:04:05.000Z"),
		VehicleData: vehicles,
		RawData:     xmlMap,
	}, nil
}

func (p *XMLParser) extractVehicleActivities(ctx context.Context, xmlMap map[string]interface{}) ([]types.VehicleActivity, error) {
	_, span := p.tracer.Start(ctx, "xml_parser.extract_vehicle_activities")
	defer span.End()

	var vehicles []types.VehicleActivity

	// Navigate through the XML structure to find VehicleActivity elements
	// The structure appears to be: Siri -> ServiceDelivery -> VehicleMonitoringDelivery -> VehicleActivity
	siri, ok := xmlMap["Siri"].(map[string]interface{})
	if !ok {
		return vehicles, nil
	}

	serviceDelivery, ok := siri["ServiceDelivery"].(map[string]interface{})
	if !ok {
		return vehicles, nil
	}

	vmDelivery, ok := serviceDelivery["VehicleMonitoringDelivery"].(map[string]interface{})
	if !ok {
		return vehicles, nil
	}

	// VehicleActivity can be a single item or an array
	var vehicleActivities []interface{}
	switch va := vmDelivery["VehicleActivity"].(type) {
	case []interface{}:
		vehicleActivities = va
	case map[string]interface{}:
		vehicleActivities = []interface{}{va}
	default:
		return vehicles, nil
	}

	for _, activity := range vehicleActivities {
		activityMap, ok := activity.(map[string]interface{})
		if !ok {
			continue
		}

		vehicle := p.parseVehicleActivity(activityMap)
		if vehicle != nil {
			vehicles = append(vehicles, *vehicle)
		}
	}

	span.SetAttributes(
		attribute.Int("extracted_vehicles", len(vehicles)),
	)

	return vehicles, nil
}

func (p *XMLParser) parseVehicleActivity(activity map[string]interface{}) *types.VehicleActivity {
	vehicle := &types.VehicleActivity{}

	// Extract RecordedAtTime and ValidUntilTime from top level
	if rat, ok := activity["RecordedAtTime"].(string); ok {
		vehicle.RecordedAtTime = rat
	}
	if vut, ok := activity["ValidUntilTime"].(string); ok {
		vehicle.ValidUntilTime = vut
	}

	// Extract MonitoredVehicleJourney data
	mvj, ok := activity["MonitoredVehicleJourney"].(map[string]interface{})
	if !ok {
		return vehicle
	}

	// Extract basic fields
	if lineRef, ok := mvj["LineRef"].(string); ok {
		vehicle.LineRef = lineRef
	}
	if dirRef, ok := mvj["DirectionRef"].(string); ok {
		vehicle.DirectionRef = dirRef
	}
	if opRef, ok := mvj["OperatorRef"].(string); ok {
		vehicle.OperatorRef = opRef
	}

	// Extract VehicleRef
	if vRef, ok := mvj["VehicleRef"].(string); ok {
		vehicle.VehicleRef = vRef
	}

	// Extract FramedVehicleJourneyRef data
	if fvjr, ok := mvj["FramedVehicleJourneyRef"].(map[string]interface{}); ok {
		if datedVJRef, ok := fvjr["DatedVehicleJourneyRef"].(string); ok {
			// Use this as additional vehicle identifier if VehicleRef is empty
			if vehicle.VehicleRef == "" {
				vehicle.VehicleRef = datedVJRef
			}
		}
	}

	// Extract origin and destination
	if originRef, ok := mvj["OriginRef"].(string); ok {
		vehicle.OriginRef = originRef
	}
	if originName, ok := mvj["OriginName"].(string); ok {
		vehicle.OriginName = formatStopName(originName)
	}
	if destRef, ok := mvj["DestinationRef"].(string); ok {
		vehicle.DestinationRef = destRef
	}
	if destName, ok := mvj["DestinationName"].(string); ok {
		vehicle.DestinationName = formatStopName(destName)
	}
	if originAimed, ok := mvj["OriginAimedDepartureTime"].(string); ok {
		vehicle.OriginAimedDepartureTime = originAimed
	}
	if destAimed, ok := mvj["DestinationAimedArrivalTime"].(string); ok {
		vehicle.DestinationAimedArrivalTime = destAimed
	}

	// Extract location data
	if location, ok := mvj["VehicleLocation"].(map[string]interface{}); ok {
		if lng, ok := location["Longitude"].(string); ok {
			if f, err := parseFloat(lng); err == nil {
				vehicle.Longitude = f
			}
		}
		if lat, ok := location["Latitude"].(string); ok {
			if f, err := parseFloat(lat); err == nil {
				vehicle.Latitude = f
			}
		}
	}

	return vehicle
}

func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// formatStopName cleans up stop names from BODS format
// Rules:
// - Double underscores (__) become " - "
// - Single underscores (_) become spaces
// Example: "Lyde_Green__Science_Park" becomes "Lyde Green - Science Park"
func formatStopName(name string) string {
	if name == "" {
		return ""
	}

	// First replace double underscores with " - "
	formatted := strings.ReplaceAll(name, "__", " - ")

	// Then replace remaining single underscores with spaces
	formatted = strings.ReplaceAll(formatted, "_", " ")

	return formatted
}

// ToJSON converts ParsedBusData to formatted JSON
func ToJSON(data *types.ParsedBusData) ([]byte, error) {
	return json.MarshalIndent(data, "", "  ")
}
