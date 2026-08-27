package obs

import (
	"context"
	"log/slog"
)

// RedactingHandler wraps a slog.Handler and scrubs every record passing through
// it. It is the only handler the engine is ever wired to.
//
// It sits at the handler layer rather than at each call site on purpose: a rule
// that depends on every future caller remembering to apply it is not a rule.
type RedactingHandler struct {
	inner slog.Handler
}

// NewRedactingHandler wraps h. Every attribute, group, message and error that
// reaches h has already been through the rules in redact.go.
func NewRedactingHandler(h slog.Handler) *RedactingHandler {
	return &RedactingHandler{inner: h}
}

func (r *RedactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return r.inner.Enabled(ctx, l)
}

func (r *RedactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, RedactText(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return r.inner.Handle(ctx, out)
}

func (r *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &RedactingHandler{inner: r.inner.WithAttrs(redactAttrs(attrs))}
}

func (r *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{inner: r.inner.WithGroup(name)}
}

func redactAttrs(attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = redactAttr(a)
	}
	return out
}

// redactAttr handles one attribute, recursing through groups and resolving
// LogValuers first — a type that formats itself is exactly where a secret would
// otherwise slip past a key-based check.
func redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()

	if v.Kind() == slog.KindGroup {
		// A sensitive key on the group itself redacts the whole subtree, so
		// slog.Group("credentials", ...) cannot leak through its children.
		if IsSensitiveKey(a.Key) {
			return slog.String(a.Key, Placeholder)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redactAttrs(v.Group())...)}
	}

	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Placeholder)
	}

	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, RedactString(a.Key, v.String()))
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			// Wrapped errors are flattened by Error(), so scrubbing the joined
			// text covers the whole chain.
			return slog.String(a.Key, RedactText(err.Error()))
		}
		if s, ok := v.Any().(interface{ String() string }); ok {
			return slog.String(a.Key, RedactString(a.Key, s.String()))
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}
