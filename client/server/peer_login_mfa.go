package server

import (
	"context"
	"errors"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	gstatus "google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/internal"
	"github.com/openzro/openzro/client/internal/profilemanager"
	"github.com/openzro/openzro/client/proto"
	"github.com/openzro/openzro/client/system"
	"github.com/openzro/openzro/management/client/peerloginmfa"
)

// defaultMFAFlowTTL bounds a flow when the server did not say how long its
// approval request lasts.
const defaultMFAFlowTTL = 5 * time.Minute

// errMFATargetChanged ends a wait whose profile or management server
// changed: the token must not reach another server.
var errMFATargetChanged = gstatus.Error(codes.FailedPrecondition,
	"the profile or management server changed while waiting for 2FA approval; sign in again")

// startPeerLoginMFA turns a login that the management server left waiting
// for the user's 2FA approval into a flow the caller waits on with
// WaitMFAApproval (ADR-0028 D6). binding and cfg are the login target the
// failed login used (see loginTarget). handled is false when loginErr is
// not such a wait. A caller that cannot open the approval link gets an
// error saying so, and no flow keeps the token. The approval URL is
// returned to the caller only; it is never logged.
func (s *Server) startPeerLoginMFA(msg *proto.WaitSSOLoginRequest, opGen uint64, binding mfaFlowBinding,
	cfg *profilemanager.Config, jwt, email, hostname string, loginErr error) (*proto.WaitSSOLoginResponse, bool, error) {
	refusal, ok := peerloginmfa.Parse(loginErr, binding.managementURL)
	if !ok || refusal.Reason != peerloginmfa.ReasonRequired {
		return nil, false, nil
	}
	if !msg.GetSupportsMfaApproval() {
		return nil, true, gstatus.Error(codes.FailedPrecondition,
			"this sign-in needs your 2FA approval, which this version of the openZro app cannot show; update openZro")
	}
	ttl := refusal.ExpiresIn
	if ttl <= 0 {
		ttl = defaultMFAFlowTTL
	}
	now := time.Now()
	id, err := s.mfaFlows.add(&mfaFlow{
		binding: binding, opGen: opGen, config: cfg, jwt: jwt, email: email, hostname: hostname,
		expiresAt: now.Add(ttl),
	}, now)
	if err != nil {
		return nil, true, err
	}
	log.Infof("device sign-in is waiting for the user's 2FA approval")
	return &proto.WaitSSOLoginResponse{
		Email:           email,
		MfaFlowId:       id,
		MfaApprovalURL:  refusal.URL,
		MfaExpiresInSec: int64(ttl / time.Second),
	}, true, nil
}

// WaitMFAApproval waits until the user approves the device login that
// WaitSSOLogin left waiting, retrying it against the management server.
// It is the daemon's active operation, so Down, a new login or any other
// action ends it, as does the caller going away. Retries use the flow's own
// configuration, and stop as soon as the daemon's profile or management
// server is no longer the flow's. The flow is single-use: it ends, with its
// token, however the wait ends.
func (s *Server) WaitMFAApproval(callerCtx context.Context, msg *proto.WaitMFAApprovalRequest) (*proto.WaitMFAApprovalResponse, error) {
	binding, _ := s.loginTarget()
	// Claiming the flow and taking over as the active operation happen
	// together under the daemon lock, and only while no newer operation
	// started since the flow's SSO login: a new login always wins over an
	// older flow, never the reverse.
	s.mutex.Lock()
	flow, err := s.mfaFlows.claim(msg.GetFlowId(), binding, time.Now())
	if err != nil {
		s.mutex.Unlock()
		return nil, err
	}
	defer s.mfaFlows.finish(flow.id)
	if flow.opGen != s.actGen {
		s.mutex.Unlock()
		return nil, gstatus.Error(codes.FailedPrecondition, "a newer sign-in replaced this one; sign in again")
	}
	if s.actCancel != nil {
		s.actCancel()
	}
	// The wait derives from the caller's context directly, so a caller that
	// is gone is seen synchronously; the daemon's shutdown is the extra,
	// asynchronous, cancellation.
	ctx, cancel := context.WithCancel(callerCtx)
	opGen := s.setActiveOpLocked(cancel)
	s.mutex.Unlock()
	defer cancel()
	jwt, email, deadline, cfg := flow.jwt, flow.email, flow.expiresAt, flow.config
	stop := context.AfterFunc(s.rootCtx, cancel)
	defer stop()
	if flow.hostname != "" {
		ctx = context.WithValue(ctx, system.DeviceNameCtxKey, flow.hostname) //nolint:staticcheck // existing key type
	}

	s.setStateIfCurrent(opGen, internal.StatusConnecting)
	err = internal.WaitPeerLoginMFA(ctx, deadline, func(ctx context.Context) error {
		if current, _ := s.loginTarget(); current != flow.binding {
			return errMFATargetChanged
		}
		return s.loginWithJWT(ctx, cfg, jwt)
	}, binding.managementURL, s.mfaPoll)
	// Only an operation that is still current publishes its outcome, so a
	// wait ended by a newer operation or by Down leaves their state alone.
	switch {
	case err == nil:
		s.setStateIfCurrent(opGen, internal.StatusIdle)
		return &proto.WaitMFAApprovalResponse{Email: email}, nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		s.setStateIfCurrent(opGen, internal.StatusNeedsLogin)
		return nil, gstatus.Error(codes.Canceled, "stopped waiting for the 2FA approval")
	case errors.Is(err, internal.ErrPeerLoginMFAExpired):
		s.setStateIfCurrent(opGen, internal.StatusNeedsLogin)
		return nil, gstatus.Error(codes.DeadlineExceeded, err.Error())
	default:
		s.setStateIfCurrent(opGen, internal.StatusNeedsLogin)
		log.Warnf("device sign-in waiting for 2FA approval ended: %v", err)
		return nil, err
	}
}

// setActiveOpLocked makes cancel the daemon's active operation, after the
// caller canceled the previous one, and returns its generation. Callers
// hold s.mutex.
func (s *Server) setActiveOpLocked(cancel context.CancelFunc) uint64 {
	s.actCancel = cancel
	s.actGen++
	return s.actGen
}

// setStateIfCurrent publishes status only while the operation opGen is
// still the daemon's active one.
func (s *Server) setStateIfCurrent(opGen uint64, status internal.StatusType) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.actGen == opGen {
		internal.CtxGetState(s.rootCtx).Set(status)
	}
}

// loginWithJWT logs in to the management server of cfg with an SSO token.
func (s *Server) loginWithJWT(ctx context.Context, cfg *profilemanager.Config, jwt string) error {
	if s.peerLogin != nil {
		return s.peerLogin(ctx, cfg, jwt)
	}
	_, err := s.loginAttemptWith(ctx, cfg, "", jwt)
	return err
}

// loginTarget returns, read together under the daemon lock, the profile and
// management server a login goes to and a private copy of the configuration
// for it. Later changes to the daemon's configuration, by a profile switch
// or in place, do not reach the copy.
func (s *Server) loginTarget() (mfaFlowBinding, *profilemanager.Config) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	binding := mfaFlowBinding{profile: s.activeProfileKey()}
	if s.config == nil {
		return binding, nil
	}
	cfg := *s.config
	if cfg.ManagementURL != nil {
		u := *cfg.ManagementURL
		cfg.ManagementURL = &u
		binding.managementURL = u.String()
	}
	return binding, &cfg
}

func (s *Server) activeProfileKey() string {
	if s.mfaProfile != nil {
		return s.mfaProfile()
	}
	prof, err := s.profileManager.GetActiveProfileState()
	if err != nil {
		return ""
	}
	return prof.Username + "/" + prof.Name
}

// reapMFAFlows drops abandoned flows, and their tokens, until ctx ends.
func (s *Server) reapMFAFlows(ctx context.Context) {
	ticker := time.NewTicker(mfaFlowReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.mfaFlows.reap(now)
		}
	}
}
