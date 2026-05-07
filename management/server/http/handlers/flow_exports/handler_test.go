package flow_exports

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	flowExports "github.com/openzro/openzro/management/server/flow_exports"
)

// TestBodyToSaveInput_GCSAndDatadogPropagate guards the fix for the
// dashboard "flow_exports: gcs bucket is required" symptom: the
// requestBody decoder + bodyToSaveInput used to drop the gcs and
// datadog fields silently because the struct only declared elastic,
// s3 and http. The handler validated against a SaveInput where GCS
// was always nil, so even a fully-populated form payload from the
// dashboard came back as 400 "bucket is required".
func TestBodyToSaveInput_GCSAndDatadogPropagate(t *testing.T) {
	payload := []byte(`{
        "name": "gcs-flow-bucket",
        "type": "gcs",
        "gcs": {
            "bucket": "cora-gcs-flow-bucket",
            "auth_kind": "service_account_json",
            "service_account_json": "{\"type\":\"service_account\"}"
        }
    }`)

	var b requestBody
	require.NoError(t, json.Unmarshal(payload, &b))
	require.Equal(t, flowExports.ExportType("gcs"), b.Type)
	require.NotNil(t, b.GCS, "decoder must keep gcs block alive")
	require.Equal(t, "cora-gcs-flow-bucket", b.GCS.Bucket)

	in := bodyToSaveInput(b, 0)
	require.NotNil(t, in.GCS, "bodyToSaveInput must surface GCS to the validator")
	require.Equal(t, "cora-gcs-flow-bucket", in.GCS.Bucket)

	// Same shape for the other late-added type (datadog) so we
	// don't ship the same regression twice.
	ddPayload := []byte(`{"name":"dd","type":"datadog","datadog":{"api_key":"abc"}}`)
	var bd requestBody
	require.NoError(t, json.Unmarshal(ddPayload, &bd))
	require.NotNil(t, bd.Datadog)
	require.Equal(t, "abc", bd.Datadog.APIKey)
	inD := bodyToSaveInput(bd, 0)
	require.NotNil(t, inD.Datadog)
}
