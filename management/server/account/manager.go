package account

import (
	"context"
	"net"
	"net/netip"
	"time"

	nbdns "github.com/openzro/openzro/dns"
	"github.com/openzro/openzro/management/domain"
	"github.com/openzro/openzro/management/server/activity"
	nbcache "github.com/openzro/openzro/management/server/cache"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/controlcenter"
	"github.com/openzro/openzro/management/server/idp"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
	"github.com/openzro/openzro/management/server/users"
	"github.com/openzro/openzro/route"
)

type ExternalCacheManager nbcache.UserDataCache

// SCIMUserInput is the manager-layer projection of a SCIM Users
// create or replace request. The SCIM HTTP handler parses the wire
// format into this struct.
type SCIMUserInput struct {
	UserName    string
	DisplayName string
	ExternalID  string
	Active      bool
	AutoGroups  []string
}

// SCIMUserPatch projects a SCIM PATCH operation set. nil pointers
// mean "do not change". The handler converts SCIM PatchOp into this
// shape.
type SCIMUserPatch struct {
	UserName    *string
	DisplayName *string
	Active      *bool
	AutoGroups  *[]string
}

type Manager interface {
	GetOrCreateAccountByUser(ctx context.Context, userId, domain string) (*types.Account, error)
	GetAccount(ctx context.Context, accountID string) (*types.Account, error)
	CreateSetupKey(ctx context.Context, accountID string, keyName string, keyType types.SetupKeyType, expiresIn time.Duration,
		autoGroups []string, usageLimit int, userID string, ephemeral bool, allowExtraDNSLabels bool) (*types.SetupKey, error)
	SaveSetupKey(ctx context.Context, accountID string, key *types.SetupKey, userID string) (*types.SetupKey, error)
	CreateUser(ctx context.Context, accountID, initiatorUserID string, key *types.UserInfo) (*types.UserInfo, error)
	DeleteUser(ctx context.Context, accountID, initiatorUserID string, targetUserID string) error
	DeleteRegularUsers(ctx context.Context, accountID, initiatorUserID string, targetUserIDs []string, userInfos map[string]*types.UserInfo) error
	InviteUser(ctx context.Context, accountID string, initiatorUserID string, targetUserID string) error
	ListSetupKeys(ctx context.Context, accountID, userID string) ([]*types.SetupKey, error)
	SaveUser(ctx context.Context, accountID, initiatorUserID string, update *types.User) (*types.UserInfo, error)
	SaveOrAddUser(ctx context.Context, accountID, initiatorUserID string, update *types.User, addIfNotExists bool) (*types.UserInfo, error)
	SaveOrAddUsers(ctx context.Context, accountID, initiatorUserID string, updates []*types.User, addIfNotExists bool) ([]*types.UserInfo, error)
	GetSetupKey(ctx context.Context, accountID, userID, keyID string) (*types.SetupKey, error)
	GetAccountByID(ctx context.Context, accountID string, userID string) (*types.Account, error)
	GetAccountMeta(ctx context.Context, accountID string, userID string) (*types.AccountMeta, error)
	GetAccountOnboarding(ctx context.Context, accountID string, userID string) (*types.AccountOnboarding, error)
	AccountExists(ctx context.Context, accountID string) (bool, error)
	GetAccountIDByUserID(ctx context.Context, userID, domain string) (string, error)
	GetAccountIDFromUserAuth(ctx context.Context, userAuth nbcontext.UserAuth) (string, string, error)
	DeleteAccount(ctx context.Context, accountID, userID string) error
	GetUserByID(ctx context.Context, id string) (*types.User, error)
	GetUserFromUserAuth(ctx context.Context, userAuth nbcontext.UserAuth) (*types.User, error)
	ListUsers(ctx context.Context, accountID string) ([]*types.User, error)
	GetPeers(ctx context.Context, accountID, userID, nameFilter, ipFilter string) ([]*nbpeer.Peer, error)
	MarkPeerConnected(ctx context.Context, peerKey string, connected bool, realIP net.IP, accountID string, streamID string) error
	DeletePeer(ctx context.Context, accountID, peerID, userID string) error
	UpdatePeer(ctx context.Context, accountID, userID string, peer *nbpeer.Peer) (*nbpeer.Peer, error)
	GetNetworkMap(ctx context.Context, peerID string) (*types.NetworkMap, error)
	GetAccessGraph(ctx context.Context, accountID, view, focusID string) (*controlcenter.GraphDTO, error)
	GetPeerNetwork(ctx context.Context, peerID string) (*types.Network, error)
	AddPeer(ctx context.Context, setupKey, userID string, peer *nbpeer.Peer) (*nbpeer.Peer, *types.NetworkMap, []*posture.Checks, error)
	CreatePAT(ctx context.Context, accountID string, initiatorUserID string, targetUserID string, tokenName string, expiresIn int) (*types.PersonalAccessTokenGenerated, error)
	DeletePAT(ctx context.Context, accountID string, initiatorUserID string, targetUserID string, tokenID string) error
	GetPAT(ctx context.Context, accountID string, initiatorUserID string, targetUserID string, tokenID string) (*types.PersonalAccessToken, error)
	GetAllPATs(ctx context.Context, accountID string, initiatorUserID string, targetUserID string) ([]*types.PersonalAccessToken, error)
	GetUsersFromAccount(ctx context.Context, accountID, userID string) (map[string]*types.UserInfo, error)

	// SCIM v2 Users surface — bypasses the IdP roundtrip because the
	// IdP is the caller. See management/server/user_scim.go.
	SCIMCreateUser(ctx context.Context, accountID, callerID string, input SCIMUserInput) (*types.User, error)
	SCIMReplaceUser(ctx context.Context, accountID, callerID, userID string, input SCIMUserInput) (*types.User, error)
	SCIMPatchUser(ctx context.Context, accountID, callerID, userID string, patch SCIMUserPatch) (*types.User, error)
	SCIMDeactivateUser(ctx context.Context, accountID, callerID, userID string) error
	SCIMListUsers(ctx context.Context, accountID, callerID, userNameFilter string, startIndex, count int) ([]*types.User, int, error)
	SCIMGetUser(ctx context.Context, accountID, callerID, userID string) (*types.User, error)

	// SCIM v2 Groups surface — see management/server/group_scim.go.
	// Membership is mapped from SCIM "members" (user IDs) onto the
	// users' AutoGroups list. The Group.Peers field stays
	// peer-centric.
	SCIMCreateGroup(ctx context.Context, accountID, callerID, displayName string, memberUserIDs []string) (*types.Group, error)
	SCIMReplaceGroup(ctx context.Context, accountID, callerID, groupID, displayName string, memberUserIDs []string) (*types.Group, error)
	SCIMRenameGroup(ctx context.Context, accountID, callerID, groupID, newName string) (*types.Group, error)
	SCIMAddGroupMember(ctx context.Context, accountID, callerID, groupID, userID string) error
	SCIMRemoveGroupMember(ctx context.Context, accountID, callerID, groupID, userID string) error
	SCIMDeleteGroup(ctx context.Context, accountID, callerID, groupID string) error
	SCIMListGroups(ctx context.Context, accountID, callerID, displayNameFilter string, startIndex, count int) ([]*types.Group, int, error)
	SCIMGetGroup(ctx context.Context, accountID, callerID, groupID string) (*types.Group, []string, error)
	GetGroup(ctx context.Context, accountId, groupID, userID string) (*types.Group, error)
	GetAllGroups(ctx context.Context, accountID, userID string) ([]*types.Group, error)
	GetGroupByName(ctx context.Context, groupName, accountID string) (*types.Group, error)
	SaveGroup(ctx context.Context, accountID, userID string, group *types.Group, create bool) error
	SaveGroups(ctx context.Context, accountID, userID string, newGroups []*types.Group, create bool) error
	DeleteGroup(ctx context.Context, accountId, userId, groupID string) error
	DeleteGroups(ctx context.Context, accountId, userId string, groupIDs []string) error
	GroupAddPeer(ctx context.Context, accountId, groupID, peerID string) error
	GroupDeletePeer(ctx context.Context, accountId, groupID, peerID string) error
	GetPeerGroups(ctx context.Context, accountID, peerID string) ([]*types.Group, error)
	GetPolicy(ctx context.Context, accountID, policyID, userID string) (*types.Policy, error)
	SavePolicy(ctx context.Context, accountID, userID string, policy *types.Policy, create bool) (*types.Policy, error)
	DeletePolicy(ctx context.Context, accountID, policyID, userID string) error
	ListPolicies(ctx context.Context, accountID, userID string) ([]*types.Policy, error)
	GetRoute(ctx context.Context, accountID string, routeID route.ID, userID string) (*route.Route, error)
	CreateRoute(ctx context.Context, accountID string, prefix netip.Prefix, networkType route.NetworkType, domains domain.List, peerID string, peerGroupIDs []string, description string, netID route.NetID, masquerade bool, metric int, groups, accessControlGroupIDs []string, enabled bool, userID string, keepRoute bool) (*route.Route, error)
	SaveRoute(ctx context.Context, accountID, userID string, route *route.Route) error
	DeleteRoute(ctx context.Context, accountID string, routeID route.ID, userID string) error
	ListRoutes(ctx context.Context, accountID, userID string) ([]*route.Route, error)
	GetNameServerGroup(ctx context.Context, accountID, userID, nsGroupID string) (*nbdns.NameServerGroup, error)
	CreateNameServerGroup(ctx context.Context, accountID string, name, description string, nameServerList []nbdns.NameServer, groups []string, primary bool, domains []string, enabled bool, userID string, searchDomainsEnabled bool) (*nbdns.NameServerGroup, error)
	SaveNameServerGroup(ctx context.Context, accountID, userID string, nsGroupToSave *nbdns.NameServerGroup) error
	DeleteNameServerGroup(ctx context.Context, accountID, nsGroupID, userID string) error
	ListNameServerGroups(ctx context.Context, accountID string, userID string) ([]*nbdns.NameServerGroup, error)
	GetDNSDomain(settings *types.Settings) string
	StoreEvent(ctx context.Context, initiatorID, targetID, accountID string, activityID activity.ActivityDescriber, meta map[string]any)
	GetEvents(ctx context.Context, accountID, userID string) ([]*activity.Event, error)
	GetDNSSettings(ctx context.Context, accountID string, userID string) (*types.DNSSettings, error)
	SaveDNSSettings(ctx context.Context, accountID string, userID string, dnsSettingsToSave *types.DNSSettings) error
	GetPeer(ctx context.Context, accountID, peerID, userID string) (*nbpeer.Peer, error)
	UpdateAccountSettings(ctx context.Context, accountID, userID string, newSettings *types.Settings) (*types.Settings, error)
	UpdateAccountOnboarding(ctx context.Context, accountID, userID string, newOnboarding *types.AccountOnboarding) (*types.AccountOnboarding, error)
	LoginPeer(ctx context.Context, login types.PeerLogin) (*nbpeer.Peer, *types.NetworkMap, []*posture.Checks, error)                // used by peer gRPC API
	SyncPeer(ctx context.Context, sync types.PeerSync, accountID string) (*nbpeer.Peer, *types.NetworkMap, []*posture.Checks, error) // used by peer gRPC API
	GetAllConnectedPeers() (map[string]struct{}, error)
	HasConnectedChannel(peerID string) bool
	GetExternalCacheManager() ExternalCacheManager
	GetPostureChecks(ctx context.Context, accountID, postureChecksID, userID string) (*posture.Checks, error)
	SavePostureChecks(ctx context.Context, accountID, userID string, postureChecks *posture.Checks, create bool) (*posture.Checks, error)
	DeletePostureChecks(ctx context.Context, accountID, postureChecksID, userID string) error
	ListPostureChecks(ctx context.Context, accountID, userID string) ([]*posture.Checks, error)
	GetIdpManager() idp.Manager
	UpdateIntegratedValidator(ctx context.Context, accountID, userID, validator string, groups []string) error
	GroupValidation(ctx context.Context, accountId string, groups []string) (bool, error)
	GetValidatedPeers(ctx context.Context, accountID string) (map[string]struct{}, error)
	SyncAndMarkPeer(ctx context.Context, accountID string, peerPubKey string, meta nbpeer.PeerSystemMeta, realIP net.IP, streamID string) (*nbpeer.Peer, *types.NetworkMap, []*posture.Checks, error)
	OnPeerDisconnected(ctx context.Context, accountID string, peerPubKey string, streamID string) error
	SyncPeerMeta(ctx context.Context, peerPubKey string, meta nbpeer.PeerSystemMeta) error
	FindExistingPostureCheck(accountID string, checks *posture.ChecksDefinition) (*posture.Checks, error)
	GetAccountIDForPeerKey(ctx context.Context, peerKey string) (string, error)
	GetAccountSettings(ctx context.Context, accountID string, userID string) (*types.Settings, error)
	DeleteSetupKey(ctx context.Context, accountID, userID, keyID string) error
	UpdateAccountPeers(ctx context.Context, accountID string)
	BufferUpdateAccountPeers(ctx context.Context, accountID string)
	BuildUserInfosForAccount(ctx context.Context, accountID, initiatorUserID string, accountUsers []*types.User) (map[string]*types.UserInfo, error)
	SyncUserJWTGroups(ctx context.Context, userAuth nbcontext.UserAuth) error
	GetStore() store.Store
	GetOrCreateAccountByPrivateDomain(ctx context.Context, initiatorId, domain string) (*types.Account, bool, error)
	UpdateToPrimaryAccount(ctx context.Context, accountId string) (*types.Account, error)
	GetOwnerInfo(ctx context.Context, accountId string) (*types.UserInfo, error)
	GetCurrentUserInfo(ctx context.Context, userAuth nbcontext.UserAuth) (*users.UserInfoWithPermissions, error)

	// MFAGateForRequest decides whether a JWT-authenticated request
	// continues, must complete a TOTP challenge, or must enroll —
	// issue #31. The auth middleware calls this after JWT validation
	// + user lookup. `jwtSessionID` identifies the current JWT
	// session (sha256 of the bearer, prefixed); `mfaSessionToken` is
	// the X-MFA-Token header value (empty when absent). When the user
	// is enrolled but does not present a valid session token bound to
	// the current JWT session, the gate returns Challenge=true.
	MFAGateForRequest(ctx context.Context, connectorID string, user *types.User, settings *types.Settings, isPAT bool, jwtSessionID, mfaSessionToken string) (*MFAGateDecision, error)

	// MFAEnabled reports whether the MFA subsystem initialized OK
	// at startup. False when the operator hasn't configured
	// DataStoreEncryptionKey or when the derivation failed; in that
	// case any enforcement-on request fails closed.
	MFAEnabled() bool

	// MFAStartEnrollment provisions a fresh TOTP secret for the
	// user. The secret is NOT persisted — instead it rides in the
	// returned `PendingToken` (HS256-signed, 15min TTL) which
	// /enroll/finish must echo back. Stateless across HA replicas.
	MFAStartEnrollment(ctx context.Context, accountID, userID string) (*MFAStartEnrollmentResult, error)

	// MFAFinishEnrollment validates the pending_enrollment_token,
	// verifies the user-supplied TOTP code against the secret it
	// carries, persists the user_mfa row (secret AES-256-GCM
	// encrypted), and mints an mfa_session_token bound to
	// `jwtSessionID`. Returns the plaintext backup codes (only
	// chance to see them) and the session token.
	MFAFinishEnrollment(ctx context.Context, accountID, userID, pendingToken, code, jwtSessionID string) (*MFAFinishEnrollmentResult, error)

	// MFAChallenge verifies a TOTP code OR a single-use backup
	// code against the stored row, with lockout after 5 consecutive
	// failures. On success mints an mfa_session_token bound to
	// `jwtSessionID` so the dashboard can resume gated calls.
	MFAChallenge(ctx context.Context, userID, code, jwtSessionID string) (*MFAChallengeOutcome, error)

	// MFADisenroll removes the user's MFA row (admin override OR
	// user-initiated reset).
	MFADisenroll(ctx context.Context, userID string) error

	// MFARegenerateBackupCodes mints fresh codes, invalidating all
	// previous ones atomically.
	MFARegenerateBackupCodes(ctx context.Context, userID string) ([]string, error)

	// MFAStatus is the read for the profile / security page.
	MFAStatus(ctx context.Context, userID string) (*MFAStatus, error)

	// MFAVerifyToken validates a challenge_token / enrollment_token /
	// pending_enrollment_token / mfa_session_token. Returns the
	// userID + JTI binding + carried secret (only set for pending
	// enrollment). Empty values + non-nil error on any failure.
	MFAVerifyToken(raw string, purpose MFATokenPurpose) (*MFATokenVerifyResult, error)

	// MFASessionValid reports whether `mfaSessionToken` is a valid
	// mfa_session_token for `userID` bound to `jwtSessionID`. Used
	// by handlers that demand MFA-verified status before performing
	// sensitive operations (disenroll, regenerate, re-enroll over an
	// existing TOTP) regardless of the operator's enforcement flag.
	MFASessionValid(userID, mfaSessionToken, jwtSessionID string) bool

	// MFAIssueChallengeToken mints a fresh challenge_token bound to
	// the supplied JWT session id. Used by sensitive-op handlers
	// (disenroll, regenerate, re-enroll) when they refuse a request
	// missing an mfa_session_token — the dashboard's 403 interceptor
	// reads the returned token from the response body and bounces the
	// browser to /mfa/challenge so the user can step up. Without
	// this, voluntary MFA users (enforcement OFF) hit a dead-end on
	// disable after a tab reset wiped sessionStorage.
	MFAIssueChallengeToken(userID, jwtSessionID string) (string, error)
}

// MFATokenPurpose identifies what a short-lived MFA token can be
// redeemed for. Mirrors mfa.TokenPurpose so the account interface
// doesn't have to import the mfa package.
type MFATokenPurpose string

// gosec G101 fires on the literal values because they end in
// "enrollment"/"challenge" and the scanner heuristic flags any
// string-literal const containing common credential keywords. These
// are purpose discriminants on a JWT claim, not credentials.
//
//nolint:gosec
const (
	MFATokenPurposeChallenge         MFATokenPurpose = "mfa_challenge"
	MFATokenPurposeEnrollment        MFATokenPurpose = "mfa_enrollment"
	MFATokenPurposePendingEnrollment MFATokenPurpose = "mfa_pending_enrollment"
	MFATokenPurposeSession           MFATokenPurpose = "mfa_session"
)

// MFATokenVerifyResult is the manager-layer view of a verified MFA
// token. Mirrors mfa.VerifyResult; lives here so the account.Manager
// interface stays free of the leaf mfa package import.
type MFATokenVerifyResult struct {
	UserID     string
	JTIBinding string
	Secret     string
}

// MFAGateDecision is the wire type the MFAGateForRequest method
// returns. Mirrors server.MFAGateDecision in shape; lives here so
// the account.Manager interface stays free of the heavier
// management/server import.
type MFAGateDecision struct {
	Pass      bool
	Challenge bool
	Enroll    bool
	Token     string
}

// MFAStartEnrollmentResult carries the otpauth:// URL the dashboard
// renders as a QR, the raw secret (text fallback for manual entry),
// and the pending_enrollment_token that /enroll/finish must echo
// back. The token replaces the legacy in-memory pending cache so
// /finish can land on any HA replica.
type MFAStartEnrollmentResult struct {
	OTPAuthURL   string `json:"otpauth_url"`
	Secret       string `json:"secret"`
	PendingToken string `json:"pending_token"`
}

// MFAFinishEnrollmentResult is the response shape of a successful
// /enroll/finish: the one-time-display backup codes + the
// mfa_session_token the dashboard sends as X-MFA-Token on
// subsequent gated calls.
type MFAFinishEnrollmentResult struct {
	BackupCodes  []string `json:"backup_codes"`
	SessionToken string   `json:"mfa_session_token"`
}

// MFAChallengeOutcome maps to the HTTP response of POST /api/mfa/
// challenge. Exactly one of (OK, Locked) carries the verdict;
// UsedBackupCode is a hint to the dashboard for the "you have N
// backup codes left, regenerate now?" nudge. SessionToken is set
// only on OK and gives the dashboard its mfa_session_token.
type MFAChallengeOutcome struct {
	OK             bool
	Locked         bool
	LockedUntil    *time.Time
	UsedBackupCode bool
	SessionToken   string
}

// MFAStatus is the response of GET /api/users/me/mfa for the
// profile / security page.
type MFAStatus struct {
	Enrolled             bool
	EnrolledAt           *time.Time
	LastVerifiedAt       *time.Time
	BackupCodesRemaining int
	Locked               bool
	LockedUntil          *time.Time
}
