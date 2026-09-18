package typesafe

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// LogLevel controls the verbosity of SDK diagnostic logging.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelOff   LogLevel = "off"
)

// DefaultLogLevel is the default log filter threshold ("warn").
const DefaultLogLevel = LogLevelWarn

// LogLevels lists the supported log levels in order from most to least verbose.
var LogLevels = []LogLevel{
	LogLevelDebug,
	LogLevelInfo,
	LogLevelWarn,
	LogLevelError,
	LogLevelOff,
}

var logLevelRank = map[LogLevel]int{
	LogLevelDebug: 0,
	LogLevelInfo:  1,
	LogLevelWarn:  2,
	LogLevelError: 3,
	LogLevelOff:   4,
}

// ParseLogLevel validates and normalizes a log level string (accepting "warning" as an alias for "warn" like Python's logging).
// Returns a *TypeSafeError for unrecognized values.
func ParseLogLevel(value string, source string) (LogLevel, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "warning" {
		normalized = "warn"
	}
	lvl := LogLevel(normalized)
	if _, ok := logLevelRank[lvl]; ok {
		return lvl, nil
	}
	return "", NewTypeSafeError(
		fmt.Sprintf("Invalid log level %q from %s. Expected one of: debug, info, warn, error, off.", value, source),
	)
}

// Logger is the pluggable logging interface used by the TypeSafe Go SDK.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

const logPrefix = "[typesafe-sdk]"

type stdLogger struct {
	l *log.Logger
}

// NewStdLogger creates a Logger backed by a standard library *log.Logger (defaults to os.Stderr).
func NewStdLogger(w io.Writer) Logger {
	if w == nil {
		w = os.Stderr
	}
	return &stdLogger{
		l: log.New(w, "", log.LstdFlags),
	}
}

func (s *stdLogger) format(level, msg string, args ...any) string {
	if len(args) == 0 {
		return fmt.Sprintf("%s %s %s", logPrefix, level, msg)
	}
	return fmt.Sprintf("%s %s %s %v", logPrefix, level, msg, args)
}

func (s *stdLogger) Debug(msg string, args ...any) { s.l.Println(s.format("DEBUG", msg, args...)) }
func (s *stdLogger) Info(msg string, args ...any)  { s.l.Println(s.format("INFO", msg, args...)) }
func (s *stdLogger) Warn(msg string, args ...any)  { s.l.Println(s.format("WARN", msg, args...)) }
func (s *stdLogger) Error(msg string, args ...any) { s.l.Println(s.format("ERROR", msg, args...)) }

type filteredLogger struct {
	sink  Logger
	level LogLevel
}

// WithLogLevelFilter wraps sink so only messages at or above level are emitted.
func WithLogLevelFilter(sink Logger, level LogLevel) Logger {
	if sink == nil {
		sink = NewStdLogger(os.Stderr)
	}
	return &filteredLogger{sink: sink, level: level}
}

func (f *filteredLogger) enabled(at LogLevel) bool {
	return logLevelRank[at] >= logLevelRank[f.level]
}

func (f *filteredLogger) Debug(msg string, args ...any) {
	if f.enabled(LogLevelDebug) {
		f.sink.Debug(msg, args...)
	}
}

func (f *filteredLogger) Info(msg string, args ...any) {
	if f.enabled(LogLevelInfo) {
		f.sink.Info(msg, args...)
	}
}

func (f *filteredLogger) Warn(msg string, args ...any) {
	if f.enabled(LogLevelWarn) {
		f.sink.Warn(msg, args...)
	}
}

func (f *filteredLogger) Error(msg string, args ...any) {
	if f.enabled(LogLevelError) {
		f.sink.Error(msg, args...)
	}
}

// ---------------------------------------------------------------------------
// Credential Header Redaction
// ---------------------------------------------------------------------------

var keyHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
}

var opaqueHeaders = map[string]bool{
	"cookie":     true,
	"set-cookie": true,
}

// redactKey masks a credential value, preserving its auth scheme (if present) and the last 4 characters
// when the secret is longer than 8 characters (matching the JS/Python SDK redaction logic).
func redactKey(value string) string {
	var scheme, secret string
	trimmed := strings.TrimSpace(value)
	if idx := strings.IndexAny(trimmed, " \t"); idx != -1 {
		scheme = trimmed[:idx]
		secret = strings.TrimSpace(trimmed[idx+1:])
	} else {
		secret = trimmed
	}
	tail := ""
	if len(secret) > 8 {
		tail = secret[len(secret)-4:]
	}
	if scheme != "" {
		return fmt.Sprintf("%s ***%s", scheme, tail)
	}
	return fmt.Sprintf("***%s", tail)
}

// RedactHeaderValue masks known secret header values:
// - "authorization", "proxy-authorization", "x-api-key" retain scheme + last 4 chars of secrets > 8 chars.
// - "cookie", "set-cookie", and any header name containing "token" or "secret" (case-insensitive) are masked as "***".
func RedactHeaderValue(name, value string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if keyHeaders[lower] {
		return redactKey(value)
	}
	if opaqueHeaders[lower] || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
		return "***"
	}
	return value
}

// RedactHeaders returns a sanitized copy of a string map of headers with all sensitive credentials masked.
func RedactHeaders(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[k] = RedactHeaderValue(k, v)
	}
	return out
}

// RedactHTTPHeader returns a sanitized map from an http.Header with all sensitive credentials masked.
func RedactHTTPHeader(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vals := range h {
		out[k] = RedactHeaderValue(k, strings.Join(vals, ", "))
	}
	return out
}
