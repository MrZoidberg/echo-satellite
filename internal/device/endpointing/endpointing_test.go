package endpointing

import (
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scriptedDetector struct{ scores []float64 }

func (*scriptedDetector) Observe([]int16) {}
func (d *scriptedDetector) SpeechScore() float64 {
	if len(d.scores) == 0 {
		return 0
	}
	score := d.scores[0]
	d.scores = d.scores[1:]
	return score
}

func TestControllerEndpointingAndInternalPause(t *testing.T) {
	detector := &scriptedDetector{scores: []float64{1, 1, 0, 0, 1, 1, 0, 0, 0, 0}}
	controller := newController(t, detector, protocol.EndpointingConfig{SpeechThreshold: .5, SpeechOnsetMS: 200, TrailingSilenceMS: 400, NoSpeechTimeoutMS: 900, MaxTurnMS: 2000})
	require.NoError(t, controller.Start(0))
	for i := range 9 {
		reason, done := controller.Observe(make([]int16, 1600)) // 100 ms
		assert.Falsef(t, done, "frame %d unexpectedly ended with %q", i, reason)
	}
	reason, done := controller.Observe(make([]int16, 1600))
	assert.True(t, done)
	assert.Equal(t, protocol.AudioStopEndpointed, reason)
}

func TestControllerNoSpeechTimeoutAndHardTimeout(t *testing.T) {
	t.Run("no speech", func(t *testing.T) {
		controller := newController(t, &scriptedDetector{}, protocol.EndpointingConfig{SpeechThreshold: .5, SpeechOnsetMS: 100, TrailingSilenceMS: 100, NoSpeechTimeoutMS: 200, MaxTurnMS: 500})
		require.NoError(t, controller.Start(0))
		_, done := controller.Observe(make([]int16, 1600))
		assert.False(t, done)
		reason, done := controller.Observe(make([]int16, 1600))
		assert.True(t, done)
		assert.Equal(t, protocol.AudioStopNoSpeech, reason)
	})
	t.Run("intermittent speech never reaches onset", func(t *testing.T) {
		controller := newController(t, &scriptedDetector{scores: []float64{1, 0, 1}}, protocol.EndpointingConfig{SpeechThreshold: .5, SpeechOnsetMS: 200, TrailingSilenceMS: 100, NoSpeechTimeoutMS: 300, MaxTurnMS: 500})
		require.NoError(t, controller.Start(0))
		controller.Observe(make([]int16, 1600))
		controller.Observe(make([]int16, 1600))
		reason, done := controller.Observe(make([]int16, 1600))
		assert.True(t, done)
		assert.Equal(t, protocol.AudioStopNoSpeech, reason)
	})
	t.Run("hard timeout", func(t *testing.T) {
		controller := newController(t, &scriptedDetector{scores: []float64{1, 1, 1}}, protocol.EndpointingConfig{SpeechThreshold: .5, SpeechOnsetMS: 100, TrailingSilenceMS: 500, NoSpeechTimeoutMS: 500, MaxTurnMS: 300})
		require.NoError(t, controller.Start(0))
		controller.Observe(make([]int16, 1600))
		controller.Observe(make([]int16, 1600))
		reason, done := controller.Observe(make([]int16, 1600))
		assert.True(t, done)
		assert.Equal(t, protocol.AudioStopTimeout, reason)
	})
}

func TestControllerEOFAndCancellation(t *testing.T) {
	controller := newController(t, &scriptedDetector{}, defaultConfig())
	require.NoError(t, controller.Start(0))
	reason, done := controller.EOF()
	assert.True(t, done)
	assert.Equal(t, protocol.AudioStopEOF, reason)
	require.NoError(t, controller.Start(0))
	controller.Cancel()
	assert.True(t, controller.Idle())
	_, done = controller.EOF()
	assert.False(t, done)
}

func TestControllerStagesConfigAtIdleBoundary(t *testing.T) {
	first := defaultConfig()
	controller := newController(t, &scriptedDetector{}, first)
	require.NoError(t, controller.Start(0))
	second := first
	second.TrailingSilenceMS = 99
	pending, err := controller.StageConfig(second)
	require.NoError(t, err)
	assert.True(t, pending)
	controller.Cancel()
	require.NoError(t, controller.Start(0))
	assert.Equal(t, second, controller.turn)
}

func TestControllerRejectsInvalidConfig(t *testing.T) {
	_, err := New(defaultConfig(), nil)
	require.Error(t, err)
	invalid := defaultConfig()
	invalid.SpeechThreshold = 2
	_, err = New(invalid, &scriptedDetector{})
	assert.Error(t, err)
}

func TestControllerCountsPreRollInHardTimeout(t *testing.T) {
	controller := newController(t, &scriptedDetector{scores: []float64{1}}, protocol.EndpointingConfig{SpeechThreshold: .5, SpeechOnsetMS: 100, TrailingSilenceMS: 100, NoSpeechTimeoutMS: 500, MaxTurnMS: 200})
	require.NoError(t, controller.Start(1600)) // 100 ms of transmitted wake pre-roll
	reason, done := controller.Observe(make([]int16, 1600))
	assert.True(t, done)
	assert.Equal(t, protocol.AudioStopTimeout, reason)
	require.Error(t, controller.Start(-1))
}

func newController(t *testing.T, detector Detector, config protocol.EndpointingConfig) *Controller {
	t.Helper()
	controller, err := New(config, detector)
	require.NoError(t, err)
	return controller
}

func defaultConfig() protocol.EndpointingConfig {
	return protocol.EndpointingConfig{SpeechThreshold: .5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}
}
