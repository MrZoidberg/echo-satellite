package buttons

import "time"

const (
	HoldThreshold  = 700 * time.Millisecond
	RepeatInterval = 200 * time.Millisecond
)

type Action string

const (
	ActionTap       Action = "tap"
	ActionHoldStart Action = "hold-start"
	ActionHoldEnd   Action = "hold-end"
	ActionRepeat    Action = "repeat"
)

type Press struct {
	Key    Key
	Action Action
	Held   time.Duration
	At     time.Time
}

type pressState struct {
	started     time.Time
	holdStarted bool
}

type Recognizer struct{ pressed map[Key]pressState }

func (r *Recognizer) Feed(key Key, value int32, at time.Time) []Press {
	if r.pressed == nil {
		r.pressed = make(map[Key]pressState)
	}
	switch key {
	case KeyVolumeDown, KeyVolumeUp:
		return r.feedVolume(key, value, at)
	case KeyMute, KeyAction:
		return r.feedTapHold(key, value, at)
	default:
		return nil
	}
}

func (r *Recognizer) feedVolume(key Key, value int32, at time.Time) []Press {
	switch value {
	case 1:
		r.pressed[key] = pressState{started: at}
		return []Press{{Key: key, Action: ActionTap, At: at}}
	case 2:
		state, ok := r.pressed[key]
		if !ok {
			return nil
		}
		return []Press{{Key: key, Action: ActionRepeat, Held: at.Sub(state.started), At: at}}
	case 0:
		delete(r.pressed, key)
	}
	return nil
}

func (r *Recognizer) feedTapHold(key Key, value int32, at time.Time) []Press {
	switch value {
	case 1:
		r.pressed[key] = pressState{started: at}
	case 0:
		state, ok := r.pressed[key]
		if !ok {
			return nil
		}
		delete(r.pressed, key)
		held := at.Sub(state.started)
		if held < HoldThreshold {
			return []Press{{Key: key, Action: ActionTap, Held: held, At: at}}
		}
		if state.holdStarted {
			return []Press{{Key: key, Action: ActionHoldEnd, Held: held, At: at}}
		}
		return []Press{{Key: key, Action: ActionHoldStart, Held: HoldThreshold, At: state.started.Add(HoldThreshold)}, {Key: key, Action: ActionHoldEnd, Held: held, At: at}}
	}
	return nil
}

func (r *Recognizer) startHold(key Key) []Press {
	state, ok := r.pressed[key]
	if !ok || state.holdStarted || (key != KeyAction && key != KeyMute) {
		return nil
	}
	state.holdStarted = true
	r.pressed[key] = state
	return []Press{{Key: key, Action: ActionHoldStart, Held: HoldThreshold, At: state.started.Add(HoldThreshold)}}
}

func (r *Recognizer) repeat(key Key, held time.Duration) []Press {
	state, ok := r.pressed[key]
	if !ok || (key != KeyVolumeDown && key != KeyVolumeUp) {
		return nil
	}
	return []Press{{Key: key, Action: ActionRepeat, Held: held, At: state.started.Add(held)}}
}
