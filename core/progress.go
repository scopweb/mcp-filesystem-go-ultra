package core

import "context"

// ProgressReporter receives a real stage of a long operation.
// done and total are item counts, not a promise about client timeouts.
type ProgressReporter func(done, total int, message string)

type progressCtxKey struct{}

// ContextWithProgress attaches a reporter. A nil reporter is ignored.
func ContextWithProgress(ctx context.Context, fn ProgressReporter) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressCtxKey{}, fn)
}

// ReportProgress emits a stage only when a reporter is present and the
// context is still active. It does nothing otherwise.
func ReportProgress(ctx context.Context, done, total int, message string) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	fn, _ := ctx.Value(progressCtxKey{}).(ProgressReporter)
	if fn == nil {
		return
	}
	fn(done, total, message)
}
