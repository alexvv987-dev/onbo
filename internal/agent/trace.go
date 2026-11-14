package agent

import "context"

// ToolTrace distinguishes the model's native function call from a server-side
// grounding lookup. It deliberately contains no arguments, identity or secrets.
type ToolTrace struct {
	Name          string `json:"name"`
	ModelSelected bool   `json:"modelSelected"`
	Executed      bool   `json:"executed"`
}

type traceKey struct{}

// ObserveTools installs an optional observer for one request. No global mutable
// logger is used, so traces from different users cannot be mixed together.
func ObserveTools(ctx context.Context, observer func(ToolTrace)) context.Context {
	return context.WithValue(ctx, traceKey{}, observer)
}

func traceTool(ctx context.Context, name string, modelSelected bool, err error) {
	if observer, ok := ctx.Value(traceKey{}).(func(ToolTrace)); ok && observer != nil {
		observer(ToolTrace{Name: name, ModelSelected: modelSelected, Executed: err == nil})
	}
}
