package stores

// NodeRunRecord is the JSON/Wails-friendly shape of a persisted node_run
// row, read back for the flows canvas's live status and RECENT list. It
// carries the same fields as models.NodeRun (the write-side shape a
// commit takes) plus EndedAt, which only exists once a run has actually been
// persisted.
type NodeRunRecord struct {
	FlowID    string `json:"flowId"`
	NodeID    string `json:"nodeId"`
	OK        bool   `json:"ok"`
	InCount   int    `json:"inCount"`
	OutCount  int    `json:"outCount"`
	DropCount int    `json:"dropCount"`
	Err       string `json:"err"`
	DurMs     int64  `json:"durMs"`
	EndedAt   int64  `json:"endedAt"`
}
