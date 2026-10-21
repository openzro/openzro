package peerloginmfa

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The server builds the refusal and the client parses it back, with the
// same reasons, codes and metadata (ADR-0028 D5).
func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Error
	}{
		{
			name: "required",
			err:  Required("https://app.example.com", "approval-token", 5*time.Minute),
			want: Error{Reason: ReasonRequired, URL: "https://app.example.com/mfa/peer-login#t=approval-token", ExpiresIn: 5 * time.Minute},
		},
		{
			name: "denied",
			err:  Denied(90 * time.Second),
			want: Error{Reason: ReasonDenied, RetryAfter: 90 * time.Second},
		},
		{
			name: "enrollment required",
			err:  EnrollmentRequired("https://app.example.com"),
			want: Error{Reason: ReasonEnrollmentRequired, URL: "https://app.example.com/"},
		},
		{
			name: "rate limited",
			err:  RateLimited(3 * time.Minute),
			want: Error{Reason: ReasonRateLimited, RetryAfter: 3 * time.Minute},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Parse(tc.err, "https://mgmt.example.com:443")
			require.True(t, ok)
			assert.Equal(t, tc.want, *got)
		})
	}
}

func TestCodes(t *testing.T) {
	assert.Equal(t, codes.FailedPrecondition, status.Code(Required("", "t", time.Minute)))
	assert.Equal(t, codes.FailedPrecondition, status.Code(Denied(time.Minute)))
	assert.Equal(t, codes.FailedPrecondition, status.Code(EnrollmentRequired("")))
	assert.Equal(t, codes.ResourceExhausted, status.Code(RateLimited(time.Minute)))
}

// Without a dashboard URL the client falls back to its management URL,
// without its port, path or credentials.
func TestParseFallsBackToManagementURL(t *testing.T) {
	got, ok := Parse(Required("", "tok", time.Minute), "https://user:pw@mgmt.example.com:33073/api")
	require.True(t, ok)
	assert.Equal(t, "https://mgmt.example.com/mfa/peer-login#t=tok", got.URL)

	got, ok = Parse(EnrollmentRequired(""), "https://mgmt.example.com")
	require.True(t, ok)
	assert.Equal(t, "https://mgmt.example.com/", got.URL)
}

// The token rides only in the URL fragment, and never in the error text,
// so logs that print the error do not leak it.
func TestTokenStaysOutOfMessages(t *testing.T) {
	err := Required("https://app.example.com", "secret-token", time.Minute)
	assert.NotContains(t, err.Error(), "secret-token")
	got, ok := Parse(err, "")
	require.True(t, ok)
	assert.NotContains(t, got.Error(), "secret-token")
	assert.NotContains(t, fmt.Sprintf("%v", got), "secret-token")
	assert.NotContains(t, fmt.Sprintf("%+v", got), "secret-token")
}

// Is recognizes a refusal even when no link can be built for it.
func TestIs(t *testing.T) {
	assert.True(t, Is(EnrollmentRequired("")))
	assert.True(t, Is(fmt.Errorf("x: %w", Denied(time.Minute))))
	assert.False(t, Is(status.Error(codes.FailedPrecondition, "other")))
	assert.False(t, Is(nil))
	_, ok := Parse(EnrollmentRequired(""), "")
	assert.False(t, ok, "no link without a URL, so Parse cannot use it")
}

// Other errors, and wrapped ones, are told apart.
func TestParseOtherErrors(t *testing.T) {
	_, ok := Parse(status.Error(codes.FailedPrecondition, "something else"), "")
	assert.False(t, ok)
	_, ok = Parse(fmt.Errorf("plain"), "")
	assert.False(t, ok)
	_, ok = Parse(nil, "")
	assert.False(t, ok)

	wrapped := fmt.Errorf("login: %w", Denied(time.Minute))
	got, ok := Parse(wrapped, "")
	require.True(t, ok)
	assert.Equal(t, ReasonDenied, got.Reason)
}

// A refusal whose URL would not be https (or http on loopback) is not
// turned into a link: the client must not open it.
func TestParseRejectsUnsafeURLs(t *testing.T) {
	_, ok := Parse(Required("http://evil.example.com", "tok", time.Minute), "")
	assert.False(t, ok)
	got, ok := Parse(Required("http://localhost:3000", "tok", time.Minute), "")
	require.True(t, ok)
	assert.Equal(t, "http://localhost:3000/mfa/peer-login#t=tok", got.URL)
}
