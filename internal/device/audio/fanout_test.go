package audio

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFanout_DeliversEveryFrameToEverySubscriber(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	source := &sourceStub{format: format, reads: []sourceRead{
		{data: encodeS16([]int16{1})}, {data: encodeS16([]int16{2})}, {err: io.EOF},
	}}
	fanout := NewFanout(newTestCapturer(t, source, CaptureConfig{Device: format, Channels: []int{0}, Preprocessor: Bypass{}, StepSamples: 1}))
	first, err := fanout.Subscribe("wake", 2)
	require.NoError(t, err)
	second, err := fanout.Subscribe("diagnostics", 2)
	require.NoError(t, err)

	require.NoError(t, fanout.Run(context.Background()))
	assert.Equal(t, []Frame{{Offset: 0, Samples: []int16{1}}, {Offset: 1, Samples: []int16{2}}}, collectFrames(first.Frames))
	assert.Equal(t, []Frame{{Offset: 0, Samples: []int16{1}}, {Offset: 1, Samples: []int16{2}}}, collectFrames(second.Frames))
	assert.Zero(t, first.Dropped())
	assert.Zero(t, second.Dropped())
	assert.Equal(t, 3, source.index, "one source read sequence must feed every subscriber")
}

func TestFanout_SubscribeAfterRunIsRejected(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	source := &sourceStub{format: format, reads: []sourceRead{{err: io.EOF}}}
	fanout := NewFanout(newTestCapturer(t, source, CaptureConfig{Device: format, Channels: []int{0}, Preprocessor: Bypass{}, StepSamples: 1}))
	require.NoError(t, fanout.Run(context.Background()))
	_, err := fanout.Subscribe("late", 1)
	assert.ErrorIs(t, err, ErrFanoutRunning)
}

func TestFanout_SlowSubscriberDropsFramesWithoutBlockingCapture(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	reads := make([]sourceRead, 0, 101)
	for i := range 100 {
		reads = append(reads, sourceRead{data: encodeS16([]int16{int16(i)})})
	}
	reads = append(reads, sourceRead{err: io.EOF})
	source := &sourceStub{format: format, reads: reads}
	fanout := NewFanout(newTestCapturer(t, source, CaptureConfig{Device: format, Channels: []int{0}, Preprocessor: Bypass{}, StepSamples: 1}))
	slow, err := fanout.Subscribe("slow", 1)
	require.NoError(t, err)

	require.NoError(t, fanout.Run(context.Background()))
	assert.Positive(t, slow.Dropped())
	assert.Equal(t, 101, source.index, "capture must run to EOF despite the full subscriber")
}

func collectFrames(frames <-chan Frame) []Frame {
	var collected []Frame
	for frame := range frames {
		collected = append(collected, frame)
	}
	return collected
}
