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
	// below are based off examples I've seen, I don't have any stats for these
	TdarrAudioCodecsPie     []TdarrPieSlice
	TdarrAudioContainersPie []TdarrPieSlice
}

// core metrics
type TdarrMetric struct {
	TotalFileCount        int              `json:"totalFileCount"`
	TotalTranscodeCount   int              `json:"totalTranscodeCount"`
	TotalHealthCheckCount int              `json:"totalHealthCheckCount"`
	SizeDiff              float64          `json:"sizeDiff"`
	Pies                  [][]interface{}  `json:"pies"`
	TdarrScore            string           `json:"tdarrScore"`
	HealthCheckScore      string           `json:"healthCheckScore"`
	AvgNumStreams         float64          `json:"avgNumberOfStreamsInVideo"`
	StreamStats           TdarrStreamStats `json:"streamStats"`
	HoldQueue             int              `json:"table0Count"`
	TranscodeQueue        int              `json:"table1Count"`
	TranscodeSuccess      int              `json:"table2Count"`
	TranscodeFailed       int              `json:"table3Count"`
	HealthCheckQueue      int              `json:"table4Count"`
	HealthCheckSuccess    int              `json:"table5Count"`
	HealthCheckFailed     int              `json:"table6Count"`
}

type TdarrStreamStatsObj struct {
	Average int64 `json:"average"`
	Highest int64 `json:"highest"`
	Total   int64 `json:"total"`
}

type TdarrStreamStats struct {
	Duration  TdarrStreamStatsObj `json:"duration"`
	BitRate   TdarrStreamStatsObj `json:"bit_rate"`
	NumFrames TdarrStreamStatsObj `json:"nb_frames"`
}

type TdarrResourceStats struct {
	Process struct {
		Uptime      int64   `json:"uptime"`
		HeapUsedMb  float64 `json:"heapUsedMB"`
		HeapTotalMb float64 `json:"heapTotalMB"`
	} `json:"process"`
	Os struct {
		CpuPercent float64 `json:"cpuPerc"`
		MemUsedGb  float64 `json:"memUsedGB"`
		MemTotalGb float64 `json:"memTotalGB"`
	} `json:"os"`
}

type TdarrNode struct {
	Id            string             `json:"_id"`
	Name          string             `json:"nodeName"`
	RemoteAddress string             `json:"remoteAddress"`
	Config        TdarrNodeConfig    `json:"config"`
	WorkerLimits  TdarrNodeJobs      `json:"workerLimits"`
	GpuSelect     string             `json:"gpuSelect"`
	NodePaused    bool               `json:"nodePaused"`
	Priority      int                `json:"priority"`
	ResourceStats TdarrResourceStats `json:"resStats"`
	QueueLengths  TdarrNodeJobs      `json:"queueLengths"`
}

type TdarrNodeConfig struct {
	ServerIp   string `json:"serverIP"`
	ServerPort string `json:"serverPort"`
	Priority   int    `json:"priority"`
}

type TdarrNodeJobs struct {
	HealthCheckCpu int `json:"healthcheckcpu"`
	HealthCheckGpu int `json:"healthcheckgpu"`
	TranscodeCpu   int `json:"transcodecpu"`
	TranscodeGpu   int `json:"transcodegpu"`
}
