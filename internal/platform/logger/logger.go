package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type ctxKey string

const (
	RequestIDKey ctxKey = "requestId"
	RideIDKey    ctxKey = "rideId"
)

// sensitiveKeys defines field names that must never be logged in cleartext.
var sensitiveKeys = map[string]bool{
	"token":         true,
	"jwt":           true,
	"authorization": true,
	"password":      true,
	"secret":        true,
	"push_token":    true,
	"pushtoken":     true,
	"phone":         true,
	"phone_number":  true,
}

// sanitizeAttr redacts or masks sensitive PII attributes.
func sanitizeAttr(groups []string, a slog.Attr) slog.Attr {
	keyLower := strings.ToLower(a.Key)
	if sensitiveKeys[keyLower] {
		return slog.String(a.Key, "[REDACTED]")
	}
	// Redact bearer tokens in strings
	if a.Value.Kind() == slog.KindString {
		strVal := a.Value.String()
		if strings.HasPrefix(strings.ToLower(strVal), "bearer ") {
			return slog.String(a.Key, "Bearer [REDACTED]")
		}
	}
	return a
}

// New creates a production JSON slog.Logger bound to a given service name.
func New(serviceName string, levelStr string, out io.Writer) *slog.Logger {
	if out == nil {
		out = os.Stdout
	}

	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: sanitizeAttr,
	}

	handler := slog.NewJSONHandler(out, opts).WithAttrs([]slog.Attr{
		slog.String("service", serviceName),
	})

	return slog.New(handler)
}

// ContextWithRequestID returns a new context carrying the request ID.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// ContextWithRideID returns a new context carrying the ride ID.
func ContextWithRideID(ctx context.Context, rideID string) context.Context {
	return context.WithValue(ctx, RideIDKey, rideID)
}

// FromContext enriches a logger with contextual IDs if present.
func FromContext(ctx context.Context, log *slog.Logger) *slog.Logger {
	if log == nil {
		log = slog.Default()
	}
	if reqID, ok := ctx.Value(RequestIDKey).(string); ok && reqID != "" {
		log = log.With(slog.String("requestId", reqID))
	}
	if rideID, ok := ctx.Value(RideIDKey).(string); ok && rideID != "" {
		log = log.With(slog.String("rideId", rideID))
	}
	return log
}
