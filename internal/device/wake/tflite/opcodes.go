// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

// SupportedOpcodes returns the stable alphabetical operator inventory accepted
// by this runtime. The returned slice is independent of package state.
func SupportedOpcodes() []string {
	return []string{
		"ADD", "BATCH_MATMUL", "CONV_2D", "DIV", "EXP", "EXPAND_DIMS",
		"FULLY_CONNECTED", "LEAKY_RELU", "LOG", "LOGISTIC",
		"MAXIMUM", "MAX_POOL_2D", "MEAN", "MINIMUM", "MUL",
		"PAD", "REDUCE_MAX", "RELU", "RELU6", "RESHAPE", "RSQRT",
		"SQRT", "SQUARE", "SQUARED_DIFFERENCE", "SQUEEZE", "SUB",
		"SUM", "TRANSPOSE",
	}
}

// Opcodes returns each operator required by the model, once, in graph order.
func (m *Model) Opcodes() []string {
	seen := make(map[string]bool)
	var out []string
	for _, graph := range m.Subgraphs {
		for _, operator := range graph.Ops {
			name := operator.Name()
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// UnsupportedOpcodes returns the model's required operators that have no
// registered kernel, once, in graph order.
func (m *Model) UnsupportedOpcodes() []string {
	seen := make(map[string]bool)
	var out []string
	for _, graph := range m.Subgraphs {
		for _, operator := range graph.Ops {
			name := operator.Name()
			if _, ok := kernels[operator.Op]; ok || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
