package discovery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
)

func marshalPairing(inst Instance) ([]byte, error) {
	data, err := json.Marshal(pairingStateFromInstance(inst))
	if err != nil {
		return nil, fmt.Errorf("encode paired gateway: %w", err)
	}
	return data, nil
}

func unmarshalPairing(data []byte, inst *Instance) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state pairingState
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("decode paired gateway: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("decode paired gateway: multiple JSON values")
	}
	*inst = state.instance()
	return inst.Validate()
}

type pairingState struct {
	ServerID string       `json:"server_id"`
	Host     string       `json:"host"`
	Port     int          `json:"port"`
	Addrs    []netip.Addr `json:"addrs"`
	TXT      TXTRecord    `json:"txt"`
}

func pairingStateFromInstance(inst Instance) pairingState {
	return pairingState(inst)
}

func (s pairingState) instance() Instance {
	return Instance(s)
}
