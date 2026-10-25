package model

import "encoding/json"

type LocationInput struct {
	Longitude float64
	Latitude  float64
	Altitude  *float64
	City      string
	Device    string
	Timestamp int64
	Timezone  *float64
}

type Quality struct {
	Status    string          `json:"status"`
	Reason    string          `json:"reason"`
	Version   *string         `json:"version"`
	CheckedAt *int64          `json:"checkedAt"`
	Evidence  json.RawMessage `json:"evidence"`
}

type Location struct {
	ID        int64    `json:"id"`
	Longitude float64  `json:"longitude"`
	Latitude  float64  `json:"latitude"`
	Altitude  float64  `json:"altitude"`
	City      string   `json:"city"`
	Device    string   `json:"device"`
	Timestamp int64    `json:"timestamp"`
	Timezone  *float64 `json:"timezone"`
	Quality   Quality  `json:"quality"`
}

type Analysis struct {
	State         string `json:"state"`
	WindowPoints  int    `json:"windowPoints"`
	UpdatedPoints int    `json:"updatedPoints"`
}

type IngestResult struct {
	Location Location `json:"location"`
	Analysis Analysis `json:"analysis"`
}

type Pagination struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"hasMore"`
}

type LocationPage struct {
	Locations  []Location `json:"locations"`
	Pagination Pagination `json:"pagination"`
}

type LocationFilter struct {
	Since, Until *int64
	Device       *string
	Project      *string
	Filtered     bool
}

type Project struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	StartTime int64  `json:"startTime"`
	EndTime   int64  `json:"endTime"`
}

type ProjectInput struct {
	Name      string
	StartTime int64
	EndTime   int64
}

type ProjectPatch struct {
	Name      *string
	StartTime *int64
	EndTime   *int64
}

type ProjectPage struct {
	Projects   []Project  `json:"projects"`
	Pagination Pagination `json:"pagination"`
}
