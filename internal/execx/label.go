package execx

import "context"

type labelKey struct{}

// WithLabel attaches a short source name such as "myjobs" to ctx. Runners
// use it in the call history, the debug log and as the file name when
// recording fixtures.
func WithLabel(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, labelKey{}, label)
}

// Label returns the label attached with WithLabel, or "".
func Label(ctx context.Context) string {
	s, _ := ctx.Value(labelKey{}).(string)
	return s
}
