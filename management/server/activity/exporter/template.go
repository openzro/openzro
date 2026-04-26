package exporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/openzro/openzro/management/server/activity"
)

// Template payload customization.
//
// The default payload of HTTPWebhook (and the default `message` field
// of Datadog) carries openZro's native event shape. Some receivers —
// internal SOC pipelines, legacy SIEMs, Slack-style channel bots —
// require a specific schema that does not match the native shape.
// Rather than ship a per-target adapter for each, we let operators
// supply a Go text/template that renders an event into the bytes the
// receiver expects.
//
// Safety stance:
//
//   - text/template alone has no exec/file/network primitives. The
//     standard control structures (range, with, if, eq) are pure and
//     bounded by the input data — and the input is our own
//     activity.Event, not user-controlled.
//   - We still cap input template size (templateMaxBytes) and output
//     size (outputMaxBytes) so a runaway range can't OOM the process
//     under high event throughput.
//   - The funcMap is small and read-only: no Go reflection, no env
//     access, no time.Now indirection.
//
// What an operator typically writes:
//
//   {{ json (dict
//     "ts" (rfc3339 .Timestamp)
//     "user" .InitiatorEmail
//     "action" .ActivityCode
//     "tenant" .AccountID
//     "extra" .Meta
//   ) }}
//
// or for a flat key=value alert format:
//
//   ts={{rfc3339 .Timestamp}} act={{.ActivityCode}} usr={{.InitiatorID}}
//   {{- with .Meta }}{{range $k, $v := .}} {{$k}}={{$v}}{{end}}{{end}}

const (
	// templateMaxBytes caps the source template length. Operators
	// pasting a 4KB Go template by accident is reasonable; a 1MB one
	// is almost certainly a mistake (or a denial-of-service attempt
	// against ourselves).
	templateMaxBytes = 4 * 1024

	// outputMaxBytes caps a single rendered event's byte length. The
	// default payload runs ~600 bytes; a malicious template that does
	// `{{range}}{{.}}{{end}}` over a recursive structure could easily
	// blow up. 256KB is generous for real receivers.
	outputMaxBytes = 256 * 1024
)

// RenderableEvent is the view exposed to a template. It mirrors
// activity.Event but renames JSON-style snake_case fields to
// PascalCase so template authors get the same names whether they
// inspect the wire format or the template input. We do this rather
// than feed the bare struct because activity.Event has unexported
// internals we do not want to surface.
type RenderableEvent struct {
	ID             uint64
	Timestamp      time.Time
	Activity       string
	ActivityCode   uint32
	Message        string
	InitiatorID    string
	InitiatorName  string
	InitiatorEmail string
	TargetID       string
	AccountID      string
	Meta           map[string]any
}

func renderableFrom(ev *activity.Event) RenderableEvent {
	return RenderableEvent{
		ID:             ev.ID,
		Timestamp:      ev.Timestamp.UTC(),
		Activity:       ev.Activity.StringCode(),
		ActivityCode:   uint32(ev.Activity),
		Message:        ev.Activity.Message(),
		InitiatorID:    ev.InitiatorID,
		InitiatorName:  ev.InitiatorName,
		InitiatorEmail: ev.InitiatorEmail,
		TargetID:       ev.TargetID,
		AccountID:      ev.AccountID,
		Meta:           ev.Meta,
	}
}

// PayloadTemplate is a compiled, reusable template. Compile once at
// boot (NewPayloadTemplate) and call Render per event from the
// exporter's flush loop.
type PayloadTemplate struct {
	tmpl *template.Template
	src  string
}

// NewPayloadTemplate compiles src and returns a renderer. Returns an
// error when src is empty, too large, or fails to parse. Callers MUST
// validate the template at config-save time so operators see the
// error in the UI, not in the activity exporter logs.
func NewPayloadTemplate(src string) (*PayloadTemplate, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("template: empty")
	}
	if len(src) > templateMaxBytes {
		return nil, fmt.Errorf("template: too large (%d bytes, max %d)", len(src), templateMaxBytes)
	}
	t, err := template.New("payload").Funcs(safeFuncMap()).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("template: parse: %w", err)
	}
	return &PayloadTemplate{tmpl: t, src: src}, nil
}

// Source returns the original template text. Useful for round-tripping
// to a config UI.
func (p *PayloadTemplate) Source() string { return p.src }

// Render evaluates the template against ev. Returns the rendered
// bytes or an error if rendering failed or exceeded outputMaxBytes.
func (p *PayloadTemplate) Render(ev *activity.Event) ([]byte, error) {
	if ev == nil {
		return nil, fmt.Errorf("template: nil event")
	}
	var buf bytes.Buffer
	if err := p.tmpl.Execute(&capWriter{w: &buf, max: outputMaxBytes}, renderableFrom(ev)); err != nil {
		return nil, fmt.Errorf("template: execute: %w", err)
	}
	return buf.Bytes(), nil
}

// capWriter writes to w until max bytes have been written, then
// returns errOutputTooLarge to abort template execution. Without this
// guard a `{{range}}` over a malformed structure can fill memory
// before Execute returns.
type capWriter struct {
	w   *bytes.Buffer
	max int
	n   int
}

var errOutputTooLarge = fmt.Errorf("template: rendered output exceeds %d bytes", outputMaxBytes)

func (c *capWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	if c.n > c.max {
		return 0, errOutputTooLarge
	}
	return c.w.Write(p)
}

// safeFuncMap is the FuncMap exposed to templates. New entries here
// must be PURE: no goroutines, no system calls, no network, no exec.
// They take and return values; no side effects.
func safeFuncMap() template.FuncMap {
	return template.FuncMap{
		// json marshals v to a JSON-encoded string. The most useful
		// helper — lets the template assemble a JSON object without
		// hand-quoting strings.
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},

		// dict builds a map from alternating key/value args. Useful
		// for inline JSON construction: `{{ json (dict "k" "v") }}`.
		"dict": func(pairs ...any) (map[string]any, error) {
			if len(pairs)%2 != 0 {
				return nil, fmt.Errorf("dict: odd number of arguments")
			}
			out := make(map[string]any, len(pairs)/2)
			for i := 0; i < len(pairs); i += 2 {
				k, ok := pairs[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key %d must be string, got %T", i, pairs[i])
				}
				out[k] = pairs[i+1]
			}
			return out, nil
		},

		// default returns d when v is the zero value. Saves a `with`
		// block for every optional field.
		"default": func(d, v any) any {
			if v == nil {
				return d
			}
			if s, ok := v.(string); ok && s == "" {
				return d
			}
			return v
		},

		// rfc3339 formats a time.Time as RFC3339Nano UTC. The shape
		// every SIEM expects.
		"rfc3339": func(t time.Time) string {
			return t.UTC().Format(time.RFC3339Nano)
		},

		// upper / lower are convenience for constructing tags.
		"upper": strings.ToUpper,
		"lower": strings.ToLower,

		// meta extracts a key from the event's Meta map and returns "" if
		// missing. Nicer than `index .Meta "x"` which yields <nil>.
		"meta": func(m map[string]any, key string) string {
			if m == nil {
				return ""
			}
			v, ok := m[key]
			if !ok || v == nil {
				return ""
			}
			return fmt.Sprintf("%v", v)
		},
	}
}

// ValidateTemplate compiles src against a synthetic event. Used by
// the API at save time so misconfigurations surface in the UI, not at
// 3am when the audit pipeline silently stops.
func ValidateTemplate(src string) error {
	p, err := NewPayloadTemplate(src)
	if err != nil {
		return err
	}
	sample := &activity.Event{
		ID:        1,
		Timestamp: time.Unix(0, 0).UTC(),
		Activity:  activity.PeerAddedByUser,
		AccountID: "sample-account",
		Meta:      map[string]any{"key": "value"},
	}
	if _, err := p.Render(sample); err != nil {
		return fmt.Errorf("template: render against sample: %w", err)
	}
	return nil
}
