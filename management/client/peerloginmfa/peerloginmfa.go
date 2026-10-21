// Package peerloginmfa is the wire contract for refusing a device login
// that needs the user's second factor (ADR-0028 D5). The management server
// builds the refusals; clients parse them. Both sides use this package, so
// the reasons, gRPC codes and metadata cannot drift apart.
//
// The approval token rides only in the URL fragment of the approval link,
// and never in an error's text, so logs that print the error do not leak it.
package peerloginmfa

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Domain is the ErrorInfo domain of every refusal.
const Domain = "openzro.io/peer-login-mfa"

// The reasons a device login is refused for its second factor.
const (
	// ReasonRequired: approve the login on the page the link opens.
	ReasonRequired = "MFA_REQUIRED"
	// ReasonDenied: the attempt was denied ("Not me"); retry after.
	ReasonDenied = "MFA_DENIED"
	// ReasonEnrollmentRequired: the user has no 2FA yet; set it up first.
	ReasonEnrollmentRequired = "MFA_ENROLLMENT_REQUIRED"
	// ReasonRateLimited: too many logins are waiting; retry after.
	ReasonRateLimited = "MFA_RATE_LIMITED"
)

// Metadata keys.
const (
	keyDashboardBaseURL = "dashboard_base_url"
	keyApprovalPath     = "approval_path"
	keyApprovalToken    = "approval_token"
	keyExpiresIn        = "expires_in"
	keyEnrollPath       = "enroll_path"
	keyRetryAfter       = "retry_after"
)

// The dashboard paths the links point at.
const (
	approvalPath = "/mfa/peer-login"
	// Signing in to the dashboard leads a user without 2FA, under the
	// enforcement this gate requires, to set it up.
	enrollPath = "/"
)

// Required refuses a login until the user approves it on the approval page.
// dashboardBaseURL may be empty; clients then use their management URL.
func Required(dashboardBaseURL, token string, expiresIn time.Duration) error {
	return build(codes.FailedPrecondition, "device sign-in needs your 2FA approval", ReasonRequired,
		withBase(dashboardBaseURL, map[string]string{
			keyApprovalPath:  approvalPath,
			keyApprovalToken: token,
			keyExpiresIn:     seconds(expiresIn),
		}))
}

// Denied refuses a login whose attempt was denied, until retryAfter.
func Denied(retryAfter time.Duration) error {
	return build(codes.FailedPrecondition, "device sign-in was denied", ReasonDenied,
		map[string]string{keyRetryAfter: seconds(retryAfter)})
}

// EnrollmentRequired refuses a login from a user with no 2FA set up.
func EnrollmentRequired(dashboardBaseURL string) error {
	return build(codes.FailedPrecondition, "set up 2FA in the openZro dashboard to sign in this device",
		ReasonEnrollmentRequired, withBase(dashboardBaseURL, map[string]string{keyEnrollPath: enrollPath}))
}

// RateLimited refuses a login while too many are waiting, until retryAfter.
func RateLimited(retryAfter time.Duration) error {
	return build(codes.ResourceExhausted, "too many device sign-ins are waiting for approval", ReasonRateLimited,
		map[string]string{keyRetryAfter: seconds(retryAfter)})
}

func build(code codes.Code, msg, reason string, md map[string]string) error {
	st, err := status.New(code, msg).WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: Domain, Metadata: md})
	if err != nil {
		// Only fails for a detail that cannot be marshaled.
		return status.Error(code, msg)
	}
	return st.Err()
}

func withBase(base string, md map[string]string) map[string]string {
	if base != "" {
		md[keyDashboardBaseURL] = base
	}
	return md
}

// seconds rounds up, so a client never retries early.
func seconds(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	return strconv.FormatInt(int64((d+time.Second-1)/time.Second), 10)
}

// Error is a parsed refusal. URL is the link to open: the approval page
// (with the token in its fragment) for ReasonRequired, the dashboard for
// ReasonEnrollmentRequired. Its Error text never contains the URL.
type Error struct {
	Reason     string
	URL        string
	ExpiresIn  time.Duration
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	switch e.Reason {
	case ReasonRequired:
		return "device sign-in needs your 2FA approval"
	case ReasonDenied:
		return "device sign-in was denied"
	case ReasonEnrollmentRequired:
		return "set up 2FA in the openZro dashboard to sign in this device"
	default:
		return "too many device sign-ins are waiting for approval"
	}
}

// Parse recognizes a refusal from this package in err, which may be
// wrapped. The link is built from the dashboard base URL the server sent,
// or from managementURL when it sent none. A refusal whose link would not
// be https:// (or http:// on loopback) is not recognized, so a client never
// opens it.
func Parse(err error, managementURL string) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	st, ok := status.FromError(err)
	if !ok {
		var se interface{ GRPCStatus() *status.Status }
		if !errors.As(err, &se) {
			return nil, false
		}
		st = se.GRPCStatus()
	}
	for _, d := range st.Details() {
		info, ok := d.(*errdetails.ErrorInfo)
		if !ok || info.GetDomain() != Domain {
			continue
		}
		return fromInfo(info, managementURL)
	}
	return nil, false
}

func fromInfo(info *errdetails.ErrorInfo, managementURL string) (*Error, bool) {
	md := info.GetMetadata()
	e := &Error{Reason: info.GetReason()}
	switch e.Reason {
	case ReasonRequired:
		token := md[keyApprovalToken]
		link, ok := link(md[keyDashboardBaseURL], managementURL, md[keyApprovalPath])
		if token == "" || !ok {
			return nil, false
		}
		e.URL = link + "#t=" + url.QueryEscape(token)
		e.ExpiresIn = durationOf(md[keyExpiresIn])
	case ReasonEnrollmentRequired:
		link, ok := link(md[keyDashboardBaseURL], managementURL, md[keyEnrollPath])
		if !ok {
			return nil, false
		}
		e.URL = link
	case ReasonDenied, ReasonRateLimited:
		e.RetryAfter = durationOf(md[keyRetryAfter])
	default:
		return nil, false
	}
	return e, true
}

// link joins the dashboard base (or the management URL's origin) with path.
func link(base, managementURL, path string) (string, bool) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", false
	}
	if base == "" {
		base = originOf(managementURL)
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return "", false
	}
	return strings.TrimRight(u.String(), "/") + path, true
}

// originOf keeps the scheme and hostname of the management URL: the
// dashboard is served on the default port of the same host.
func originOf(managementURL string) string {
	u, err := url.Parse(managementURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := u.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func durationOf(s string) time.Duration {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
