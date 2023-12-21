package collector

type TdarrMetricRequest struct {
	Data TdarrDataRequest `json:"data"`
}

type TdarrDataRequest struct {
	Collection string `json:"collection"`
	Mode       string `json:"mode"`
	DocId      string `json:"docID"`
}

type TdarrPieItem struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// type TdarrTranscodePie struct {
// 	TranscodeSuccess int
// 	NotRequired      int
// 	TranscodeError   int
// }

// type TdarrHealthPie struct {
// 	Queued  int
// 	Success int
// 	Error   int
// 	Ignored int
// }

type TdarrPie struct {
	LibraryName           string
	LibraryId             string
	NumFiles              int
	NumTranscodes         int
	SpaceSavedGB          float64
	NumHealthChecks       int
	TdarrTranscodePie     []TdarrPieItem
	TdarrHealthPie        []TdarrPieItem
	TdarrVideoCodecs      []TdarrPieItem
	TdarrVideoContainers  []TdarrPieItem
	TdarrVideoResolutions []TdarrPieItem
}

type TdarrDataResponse struct {
	Pies             [][]interface{} `json:"pies"`
	TdarrScore       string          `json:"tdarrScore"`
	HealthCheckScore string          `json:"healthCheckScore"`
}
