package collector

type TdarrMetricRequest struct {
	Data TdarrDataRequest `json:"data"`
}

type TdarrDataRequest struct {
	Collection string `json:"collection"`
	Mode       string `json:"mode"`
	DocId      string `json:"docID"`
}

type TdarrPieSlice struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type TdarrPie struct {
	LibraryName              string //label
	LibraryId                string //label
	NumFiles                 int
	NumTranscodes            int
	NumHealthChecks          int
	SpaceSavedGB             float64
	TdarrTranscodePie        []TdarrPieSlice
	TdarrHealthPie           []TdarrPieSlice
	TdarrVideoCodecsPie      []TdarrPieSlice
	TdarrVideoContainersPie  []TdarrPieSlice
	TdarrVideoResolutionsPie []TdarrPieSlice
}

// core metrics
type TdarrMetric struct {
	TotalFileCount        int             `json:"totalFileCount"`
	TotalTranscodeCount   int             `json:"totalTranscodeCount"`
	TotalHealthCheckCount int             `json:"totalHealthCheckCount"`
	SizeDiff              float64         `json:"sizeDiff"`
	Pies                  [][]interface{} `json:"pies"`
	TdarrScore            string          `json:"tdarrScore"`
	HealthCheckScore      string          `json:"healthCheckScore"`
}
