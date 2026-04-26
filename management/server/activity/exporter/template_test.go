package exporter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/activity"
)

func templateSampleEvent() *activity.Event {
	return &activity.Event{
		ID:             42,
		Timestamp:      time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
		Activity:       activity.PeerAdmissionDenied,
		InitiatorID:    "u1",
		InitiatorName:  "Alice",
		InitiatorEmail: "alice@example.test",
		TargetID:       "peer-1",
		AccountID:      "acct-1",
		Meta:           map[string]any{"reason": "non-compliant", "host": "laptop"},
	}
}

// TestPayloadTemplate_Empty rejects empty / whitespace-only templates.
// The factory uses presence to decide whether to enable the renderer
// — accepting an empty template would silently disable customization.
func TestPayloadTemplate_Empty(t *testing.T) {
	_, err := NewPayloadTemplate("")
	require.Error(t, err)
	_, err = NewPayloadTemplate("   \n  ")
	require.Error(t, err)
}

// TestPayloadTemplate_TooLarge enforces the 4KB source cap. Bigger
// templates almost certainly indicate a paste accident or DoS.
func TestPayloadTemplate_TooLarge(t *testing.T) {
	src := strings.Repeat("a", templateMaxBytes+1)
	_, err := NewPayloadTemplate(src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

// TestPayloadTemplate_BadSyntax surfaces parse errors. Operators see
// these at save time when the API uses ValidateTemplate.
func TestPayloadTemplate_BadSyntax(t *testing.T) {
	_, err := NewPayloadTemplate("{{ no_such_func }}")
	require.Error(t, err)
}

// TestPayloadTemplate_RendersJSON exercises the canonical use case:
// build a JSON payload via dict + json. This is what most SIEM
// receivers will look like.
func TestPayloadTemplate_RendersJSON(t *testing.T) {
	src := `{{ json (dict "ts" (rfc3339 .Timestamp) "user" .InitiatorEmail "act" .Activity "tenant" .AccountID) }}`
	tmpl, err := NewPayloadTemplate(src)
	require.NoError(t, err)
	out, err := tmpl.Render(templateSampleEvent())
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "2026-04-26T10:00:00Z", got["ts"])
	assert.Equal(t, "alice@example.test", got["user"])
	assert.Equal(t, "peer.admission.deny", got["act"])
	assert.Equal(t, "acct-1", got["tenant"])
}

// TestPayloadTemplate_FlatFormat exercises the alternative use case:
// key=value alert lines. Some legacy SIEMs and chat receivers want
// this shape.
func TestPayloadTemplate_FlatFormat(t *testing.T) {
	src := `act={{.Activity}} usr={{.InitiatorID}} acct={{.AccountID}}{{range $k, $v := .Meta}} {{$k}}={{$v}}{{end}}`
	tmpl, err := NewPayloadTemplate(src)
	require.NoError(t, err)
	out, err := tmpl.Render(templateSampleEvent())
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "act=peer.admission.deny")
	assert.Contains(t, got, "usr=u1")
	assert.Contains(t, got, "acct=acct-1")
	// Meta order is non-deterministic; only assert the keys are present.
	assert.Contains(t, got, "reason=non-compliant")
	assert.Contains(t, got, "host=laptop")
}

// TestPayloadTemplate_DefaultHelper proves `default` swaps in a value
// when the field is empty. Saves verbose `with` blocks for optional
// fields like InitiatorEmail (set only on dashboard logins).
func TestPayloadTemplate_DefaultHelper(t *testing.T) {
	src := `{{ default "anon" .InitiatorEmail }}`
	tmpl, err := NewPayloadTemplate(src)
	require.NoError(t, err)
	ev := templateSampleEvent()
	ev.InitiatorEmail = ""
	out, err := tmpl.Render(ev)
	require.NoError(t, err)
	assert.Equal(t, "anon", string(out))
}

// TestPayloadTemplate_MetaHelper proves the meta helper returns "" for
// missing keys — nicer than `index .Meta "k"` which yields "<nil>".
func TestPayloadTemplate_MetaHelper(t *testing.T) {
	src := `{{ meta .Meta "reason" }}|{{ meta .Meta "absent" }}`
	tmpl, err := NewPayloadTemplate(src)
	require.NoError(t, err)
	out, err := tmpl.Render(templateSampleEvent())
	require.NoError(t, err)
	assert.Equal(t, "non-compliant|", string(out))
}

// TestPayloadTemplate_OutputCap aborts execution when a template tries
// to emit more than outputMaxBytes. Without this, a malicious or
// buggy template could OOM the process under high event throughput.
//
// We construct a Meta entry of length > outputMaxBytes and emit it;
// the cap fires when capWriter sees the single oversized write.
func TestPayloadTemplate_OutputCap(t *testing.T) {
	tmpl, err := NewPayloadTemplate(`{{ index .Meta "blob" }}`)
	require.NoError(t, err)
	ev := templateSampleEvent()
	ev.Meta = map[string]any{"blob": strings.Repeat("x", outputMaxBytes+1)}
	_, err = tmpl.Render(ev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

// TestValidateTemplate is what the config-save API will call. It must
// reject syntactically broken templates AND templates that error at
// runtime against a synthetic event (e.g. a typo in a field name that
// works syntactically but blows up on access).
func TestValidateTemplate(t *testing.T) {
	require.NoError(t, ValidateTemplate(`{{.Activity}}`))
	require.Error(t, ValidateTemplate(`{{ .NotAField }}`))
	require.Error(t, ValidateTemplate(``))
}
