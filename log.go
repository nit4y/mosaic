package mosaic

import "log/slog"

// Logger is the pipeline's verbosity-gated logging surface. The library
// never logs on its own: a caller builds a Logger with NewLogger, passing
// their own *slog.Logger and whether verbose logging is on, then hands it
// to the Generate* functions.
//
// Every method funnels through enabled(), so a nil *Logger, a nil
// underlying logger, or verbose=false all make logging a no-op. That makes
// passing a Logger always safe (including nil) and logging fully opt-in.
type Logger struct {
	logger  *slog.Logger
	verbose bool
}

// NewLogger returns a verbosity-gated logger wrapping the caller's
// *slog.Logger. If logger is nil or verbose is false, every method is a
// no-op. The returned *Logger is safe to pass and call even when nil.
func NewLogger(logger *slog.Logger, verbose bool) *Logger {
	return &Logger{logger: logger, verbose: verbose}
}

// enabled is the single gate every log method checks: emit only when a
// logger was supplied and verbose is on. A disabled logger costs just this.
func (l *Logger) enabled() bool {
	return l != nil && l.verbose && l.logger != nil
}

// Info logs at info level when verbose logging is enabled, else no-ops.
func (l *Logger) Info(msg string, args ...any) {
	if l.enabled() {
		l.logger.Info(msg, args...)
	}
}

// Error logs at error level when verbose logging is enabled, else no-ops.
// (Errors are also returned to the caller; this is diagnostic only.)
func (l *Logger) Error(msg string, args ...any) {
	if l.enabled() {
		l.logger.Error(msg, args...)
	}
}

// Warn logs at warn level when verbose logging is enabled, else no-ops.
func (l *Logger) Warn(msg string, args ...any) {
	if l.enabled() {
		l.logger.Warn(msg, args...)
	}
}

// Debug logs at debug level when verbose logging is enabled, else no-ops.
func (l *Logger) Debug(msg string, args ...any) {
	if l.enabled() {
		l.logger.Debug(msg, args...)
	}
}

// With returns a Logger that adds the given key/value context to every
// subsequent message, preserving the verbose setting. It is nil-safe.
func (l *Logger) With(args ...any) *Logger {
	if l == nil || l.logger == nil {
		return l
	}
	return &Logger{logger: l.logger.With(args...), verbose: l.verbose}
}
