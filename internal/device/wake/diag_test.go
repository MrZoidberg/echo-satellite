package wake

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshotHasStableJSONFieldNames(t *testing.T) {
	encoded, err := json.Marshal(Snapshot{})
	require.NoError(t, err)
	for _, field := range []string{
		"active_model_id", "model_kind", "languages", "wake_threshold", "vad_enabled",
		"vad_threshold", "vad_lookback_ms", "last_wake_score", "last_instantaneous_vad_score",
		"last_effective_vad_score", "max_wake_score", "wake_count",
		"rejected_high_wake_low_vad_count", "steps_processed", "frames_dropped", "wake_inference", "vad_inference",
	} {
		require.Contains(t, string(encoded), `"`+field+`"`)
	}
}
