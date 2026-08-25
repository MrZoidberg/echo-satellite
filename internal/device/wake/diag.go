package wake

// TimingSnapshot reports observed inference latency in milliseconds.
type TimingSnapshot struct {
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	MaxMS float64 `json:"max_ms"`
}

// Snapshot is the stable diagnostics shape shared by command output and periodic logs.
type Snapshot struct {
	ActiveModelID         string         `json:"active_model_id"`
	ModelKind             Kind           `json:"model_kind"`
	Languages             []string       `json:"languages"`
	WakeThreshold         float64        `json:"wake_threshold"`
	VADEnabled            bool           `json:"vad_enabled"`
	VADThreshold          float64        `json:"vad_threshold"`
	VADLookbackMS         int            `json:"vad_lookback_ms"`
	LastWakeScore         float64        `json:"last_wake_score"`
	LastInstantVADScore   float64        `json:"last_instantaneous_vad_score"`
	LastEffectiveVADScore float64        `json:"last_effective_vad_score"`
	MaxWakeScore          float64        `json:"max_wake_score"`
	WakeCount             uint64         `json:"wake_count"`
	RejectedLowVAD        uint64         `json:"rejected_high_wake_low_vad_count"`
	StepsProcessed        uint64         `json:"steps_processed"`
	FramesDropped         uint64         `json:"frames_dropped"`
	WakeInference         TimingSnapshot `json:"wake_inference"`
	VADInference          TimingSnapshot `json:"vad_inference"`
}
