package collector

import (
	"encoding/json"
	"testing"
)

// TestTdarrMetric_DecodesIssue126Payload decodes the exact StatisticsJSONDB
// response from issue #126, which mixes integer and float-encoded counts
// (847.0) that an int-typed field would reject, failing the whole decode.
func TestTdarrMetric_DecodesIssue126Payload(t *testing.T) {
	t.Parallel()
	body := []byte(`{
  "_id": "statistics",
  "totalFileCount": 2431,
  "totalTranscodeCount": 847.0,
  "totalHealthCheckCount": 1468.0,
  "sizeDiff": 86.57081179134545,
  "DBFetchTime": "1s",
  "DBLoadStatus": "Stable",
  "DBQueue": 0,
  "pies": [],
  "tdarrScore": "35.12",
  "healthCheckScore": "100.0",
  "processWarning": "",
  "table0Count": 0,
  "table1Count": 1570,
  "table2Count": 853,
  "table3Count": 6,
  "table4Count": 0,
  "table5Count": 743,
  "table6Count": 0,
  "table0ViewableCount": 0,
  "table1ViewableCount": 1570,
  "table2ViewableCount": 853,
  "table3ViewableCount": 6,
  "table4ViewableCount": 0,
  "table5ViewableCount": 743,
  "table6ViewableCount": 0,
  "table7ViewableCount": 2364
}`)
	var m TdarrMetric
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode issue #126 payload: %v", err)
	}
	if m.TotalFileCount != 2431 || m.TotalTranscodeCount != 847 || m.TotalHealthCheckCount != 1468 {
		t.Errorf("totals = (%v, %v, %v), want (2431, 847, 1468)",
			m.TotalFileCount, m.TotalTranscodeCount, m.TotalHealthCheckCount)
	}
	if m.TranscodeQueue != 1570 || m.HealthCheckSuccess != 743 {
		t.Errorf("table counts = (table1 %v, table5 %v), want (1570, 743)", m.TranscodeQueue, m.HealthCheckSuccess)
	}
}
