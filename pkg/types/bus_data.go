package types

type ParsedBusData struct {
	LineRef     string                 `json:"line_ref"`
	Timestamp   string                 `json:"timestamp"`
	VehicleData []VehicleActivity      `json:"vehicle_activities"`
	RawData     map[string]interface{} `json:"raw_data,omitempty"`
}

type VehicleActivity struct {
	VehicleRef                  string  `json:"vehicle_ref"`
	LineRef                     string  `json:"line_ref"`
	DirectionRef                string  `json:"direction_ref"`
	OperatorRef                 string  `json:"operator_ref"`
	OriginRef                   string  `json:"origin_ref"`
	OriginName                  string  `json:"origin_name"`
	DestinationRef              string  `json:"destination_ref"`
	DestinationName             string  `json:"destination_name"`
	OriginAimedDepartureTime    string  `json:"origin_aimed_departure_time"`
	DestinationAimedArrivalTime string  `json:"destination_aimed_arrival_time"`
	Longitude                   float64 `json:"longitude"`
	Latitude                    float64 `json:"latitude"`
	RecordedAtTime              string  `json:"recorded_at_time"`
	ValidUntilTime              string  `json:"valid_until_time"`
}
