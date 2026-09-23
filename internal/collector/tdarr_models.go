package collector

type TdarrMetricRequest struct {
	Data TdarrDataRequest `json:"data"`
}

type TdarrDataRequest struct {
	Collection string         `json:"collection"`
	Mode       string         `json:"mode"`
	DocId      string         `json:"docID"`
	Obj        map[string]any `json:"obj"`
}

type TdarrPieDataRequest struct {
	Data struct {
		LibraryId   string `json:"libraryId"`
		libraryName string `json:"-"`
	} `json:"data"`
}

type TdarrPieSlice struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// core metrics
type TdarrMetric struct {
	TotalFileCount        float64          `json:"totalFileCount"`
	TotalTranscodeCount   float64          `json:"totalTranscodeCount"`
	TotalHealthCheckCount float64          `json:"totalHealthCheckCount"`
	SizeDiff              float64          `json:"sizeDiff"`
	TdarrScore            string           `json:"tdarrScore"`
	HealthCheckScore      string           `json:"healthCheckScore"`
	AvgNumStreams         float64          `json:"avgNumberOfStreamsInVideo"`
	StreamStats           TdarrStreamStats `json:"streamStats"`
	// Per-bucket counts for cache invalidation. Returned by the StatisticsJSONDB cruddb endpoint.
	// These map to Tdarr UI buckets: table0=Hold, table1=Transcode queue,
	// table2=Transcode success+not required, table3=Transcode error+cancelled,
	// table4=Health check queue, table5=Health check healthy, table6=Health check error+cancelled.
	// Older Tdarr versions may omit these fields; Go's JSON decoder defaults them to 0,
	// which means 0==0 comparisons never trigger spurious refetches (graceful degradation).
	HoldQueue          float64 `json:"table0Count"`
	TranscodeQueue     float64 `json:"table1Count"`
	TranscodeSuccess   float64 `json:"table2Count"` // includes "not required" per Tdarr UI grouping
	TranscodeFailed    float64 `json:"table3Count"` // includes "cancelled"
	HealthCheckQueue   float64 `json:"table4Count"`
	HealthCheckSuccess float64 `json:"table5Count"`
	HealthCheckFailed  float64 `json:"table6Count"` // includes "cancelled"
}

// TdarrServerStatus decodes GET /api/v2/status. Only the fields surfaced as
// metrics/labels are mapped; isProduction/buildDate are intentionally omitted.
// uptime is Tdarr's Node.js process.uptime(), i.e. seconds.
type TdarrServerStatus struct {
	Status  string  `json:"status"`
	Version string  `json:"version"`
	Os      string  `json:"os"`
	Uptime  float64 `json:"uptime"`
}

// new api `api/v2/stats/get-pies` support
type TdarrLibraryInfo struct {
	LibraryId string `json:"_id"`
	Name      string `json:"name"`
}

type TdarrPieStats struct {
	PieStats    TdarrPieStat `json:"pieStats"`
	libraryName string
	libraryId   string
	// NormalizedTranscodes maps cleaned transcode status labels to counts.
	// Populated by normalizePieStatuses after fetch; covers the full known enum (zeros included).
	NormalizedTranscodes map[string]float64
	// NormalizedHealthChecks maps cleaned health check status labels to counts.
	// Populated by normalizePieStatuses after fetch; covers the full known enum (zeros included).
	NormalizedHealthChecks map[string]float64
}

type TdarrPieStat struct {
	TotalFiles            float64             `json:"totalFiles"`
	TotalTranscodeCount   float64             `json:"totalTranscodeCount"`
	SizeDiff              float64             `json:"sizeDiff"`
	TotalHealthCheckCount float64             `json:"totalHealthCheckCount"`
	Status                TdarrPieStatusSlice `json:"status"`
	Video                 TdarrPieVideoSlice  `json:"video"`
	Audio                 TdarrPieVideoSlice  `json:"audio"`
}

type TdarrPieStatusSlice struct {
	Transcode   []TdarrPieSlice `json:"transcode"`
	HealthCheck []TdarrPieSlice `json:"healthCheck"`
}

type TdarrPieVideoSlice struct {
	Codecs      []TdarrPieSlice `json:"codecs"`
	Containers  []TdarrPieSlice `json:"containers"`
	Resolutions []TdarrPieSlice `json:"resolutions"`
}

type TdarrStreamStatsObj struct {
	Average float64 `json:"average"`
	Highest float64 `json:"highest"`
	Total   float64 `json:"total"`
}

type TdarrStreamStats struct {
	Duration  TdarrStreamStatsObj `json:"duration"`
	BitRate   TdarrStreamStatsObj `json:"bit_rate"`
	NumFrames TdarrStreamStatsObj `json:"nb_frames"`
}

type TdarrResourceStats struct {
	Process struct {
		Uptime      float64 `json:"uptime"`
		HeapUsedMb  string  `json:"heapUsedMB"`
		HeapTotalMb string  `json:"heapTotalMB"`
	} `json:"process"`
	Os struct {
		CpuPercent string `json:"cpuPerc"`
		MemUsedGb  string `json:"memUsedGB"`
		MemTotalGb string `json:"memTotalGB"`
	} `json:"os"`
}

type TdarrNode struct {
	Id              string                      `json:"_id"`
	Name            string                      `json:"nodeName"`
	RemoteAddress   string                      `json:"remoteAddress"`
	Config          TdarrNodeConfig             `json:"config"`
	WorkerLimits    TdarrNodeJobs               `json:"workerLimits"`
	GpuSelect       string                      `json:"gpuSelect"`
	Paused          bool                        `json:"nodePaused"`
	Priority        float64                     `json:"priority"`
	Workers         map[string]TdarrNodeWorkers `json:"workers"`
	ResourceStats   TdarrResourceStats          `json:"resStats"`
	QueueLengths    TdarrNodeJobs               `json:"queueLengths"`
	MaxGpuWorkers   float64                     `json:"maxGpuWorkers"`
	ScheduleEnabled bool                        `json:"scheduleEnabled"`
	AllowGpuDoCpu   bool                        `json:"allowGpuDoCpu"`
}

type TdarrNodeConfig struct {
	ServerIp   string  `json:"serverIP"`
	ServerPort string  `json:"serverPort"`
	Priority   float64 `json:"priority"`
	Pid        float64 `json:"processPid"`
}

type TdarrNodeJobs struct {
	HealthCheckCpu float64 `json:"healthcheckcpu"`
	HealthCheckGpu float64 `json:"healthcheckgpu"`
	TranscodeCpu   float64 `json:"transcodecpu"`
	TranscodeGpu   float64 `json:"transcodegpu"`
}

type TdarrNodeWorkers struct {
	Id                 string  `json:"_id"`
	WorkerType         string  `json:"workerType"`
	FlowWorker         bool    `json:"isFlowWorker"`
	Idle               bool    `json:"idle"`
	File               string  `json:"file"`
	OriginalfileSizeGb float64 `json:"originalfileSizeInGbytes"`
	Percentage         float64 `json:"percentage"`
	Fps                float64 `json:"fps"`
	Eta                string  `json:"ETA"`
	Status             string  `json:"status"`
	StatusTs           float64 `json:"statusTs"`
	Job                struct {
		Version   string  `json:"version"`
		StartTime float64 `json:"start"`
		Type      string  `json:"type"`
		JobId     string  `json:"jobId"`
	} `json:"job"`
	Process struct {
		Connected bool    `json:"connected"`
		Pid       float64 `json:"pid"`
		CliType   string  `json:"cliType"`
	} `json:"process"`
	LastPluginDetails struct {
		Source         string `json:"source"`
		Id             string `json:"id"`
		PositionNumber string `json:"number"`
	} `json:"lastPluginDetails"`
	StartTime        float64 `json:"startTime"` // start time of current processing step (plugin or flow step)
	OutputFileSizeGb float64 `json:"outputFileSizeInGbytes"`
	EstSizeGb        float64 `json:"estSize"`
}

type tdarrCacheTotals struct {
	totalFileCount        float64
	totalTranscodeCount   float64
	totalHealthCheckCount float64
	holdQueue             float64
	transcodeQueue        float64
	transcodeSuccess      float64
	transcodeFailed       float64
	healthCheckQueue      float64
	healthCheckSuccess    float64
	healthCheckFailed     float64
}
