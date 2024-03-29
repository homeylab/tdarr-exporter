package collector

type TdarrMetricRequest struct {
	Data TdarrDataRequest `json:"data"`
}

type TdarrDataRequest struct {
	Collection string                 `json:"collection"`
	Mode       string                 `json:"mode"`
	DocId      string                 `json:"docID"`
	Obj        map[string]interface{} `json:"obj"`
}

type TdarrPieSlice struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type TdarrPie struct {
	LibraryName              string //label
	LibraryId                string //label
	NumFiles                 float64
	NumTranscodes            float64
	NumHealthChecks          float64
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
	// appears we can get below in other places and may not be necessary
	// HoldQueue             int              `json:"table0Count"`
	// TranscodeQueue        int              `json:"table1Count"`
	// TranscodeSuccess      int              `json:"table2Count"`
	// TranscodeFailed       int              `json:"table3Count"`
	// HealthCheckQueue      int              `json:"table4Count"`
	// HealthCheckSuccess    int              `json:"table5Count"`
	// HealthCheckFailed     int              `json:"table6Count"`
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
		Uptime      int64  `json:"uptime"`
		HeapUsedMb  string `json:"heapUsedMB"`
		HeapTotalMb string `json:"heapTotalMB"`
	} `json:"process"`
	Os struct {
		CpuPercent string `json:"cpuPerc"`
		MemUsedGb  string `json:"memUsedGB"`
		MemTotalGb string `json:"memTotalGB"`
	} `json:"os"`
}

type TdarrNode struct {
	Id            string                      `json:"_id"`
	Name          string                      `json:"nodeName"`
	RemoteAddress string                      `json:"remoteAddress"`
	Config        TdarrNodeConfig             `json:"config"`
	WorkerLimits  TdarrNodeJobs               `json:"workerLimits"`
	GpuSelect     string                      `json:"gpuSelect"`
	Paused        bool                        `json:"nodePaused"`
	Priority      int                         `json:"priority"`
	Workers       map[string]TdarrNodeWorkers `json:"workers"`
	ResourceStats TdarrResourceStats          `json:"resStats"`
	QueueLengths  TdarrNodeJobs               `json:"queueLengths"`
}

type TdarrNodeConfig struct {
	ServerIp   string `json:"serverIP"`
	ServerPort string `json:"serverPort"`
	Priority   int    `json:"priority"`
	Pid        int    `json:"processPid"`
}

type TdarrNodeJobs struct {
	HealthCheckCpu int `json:"healthcheckcpu"`
	HealthCheckGpu int `json:"healthcheckgpu"`
	TranscodeCpu   int `json:"transcodecpu"`
	TranscodeGpu   int `json:"transcodegpu"`
}

type TdarrNodeWorkers struct {
	Id                 string  `json:"_id"`
	WorkerType         string  `json:"workerType"`
	FlowWorker         bool    `json:"isFlowWorker"`
	Idle               bool    `json:"idle"`
	File               string  `json:"file"`
	OriginalfileSizeGb float64 `json:"originalfileSizeInGbytes"`
	Percentage         float64 `json:"percentage"`
	Fps                int     `json:"fps"`
	Eta                string  `json:"ETA"`
	Status             string  `json:"status"`
	StatusTs           int64   `json:"statusTs"`
	Job                struct {
		Version   string `json:"version"`
		StartTime int64  `json:"start"`
		Type      string `json:"type"`
		JobId     string `json:"jobId"`
	} `json:"job"`
	Process struct {
		Connected bool   `json:"connected"`
		Pid       int    `json:"pid"`
		CliType   string `json:"cliType"`
	} `json:"process"`
	LastPluginDetails struct {
		Source         string `json:"source"`
		Id             string `json:"id"`
		PositionNumber string `json:"number"`
	} `json:"lastPluginDetails"`
	StartTime        int64   `json:"startTime"` // start time of current plugin step
	OutputFileSizeGb float64 `json:"outputFileSizeInGbytes"`
	EstSizeGb        float64 `json:"estSize"`
}
