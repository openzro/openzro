package server

import (
	"context"
	"errors"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	gstatus "google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/internal"
	"github.com/openzro/openzro/client/proto"
	"github.com/openzro/openzro/client/system"
	"github.com/openzro/openzro/management/client/peerloginmfa"
)

// defaultMFAFlowTTL bounds a flow when the server did not say how long its
// approval request lasts.
const defaultMFAFlowTTL = 5 * time.Minute

// startPeerLoginMFA turns a login that the management server left waiting
// for the user's 2FA approval into a flow the caller waits on with
// WaitMFAApproval (ADR-0028 D6). handled is false when loginErr is not such
// a wait. A caller that cannot open the approval link gets an error saying
// so, and no flow keeps the token. The approval URL is returned to the
// caller only; it is never logged.
func (s *Server) startPeerLoginMFA(_ context.Context, msg *proto.WaitSSOLoginRequest, jwt, email, hostname string,
	loginErr error) (*proto.WaitSSOLoginResponse, bool, error) {
	binding := s.mfaBinding()
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
	id, err := s.mfaFlows.add(binding, jwt, email, hostname, now.Add(ttl), now)
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
// The flow is single-use: it ends, with its token, however the wait ends.
func (s *Server) WaitMFAApproval(callerCtx context.Context, msg *proto.WaitMFAApprovalRequest) (*proto.WaitMFAApprovalResponse, error) {
	binding := s.mfaBinding()
	flow, err := s.mfaFlows.claim(msg.GetFlowId(), binding, time.Now())
	if err != nil {
		return nil, err
	}
	defer s.mfaFlows.finish(flow.id)
	jwt, email, deadline := flow.jwt, flow.email, flow.expiresAt

	ctx, cancel := context.WithCancel(callerCtx)
	defer cancel()
	stop := context.AfterFunc(s.rootCtx, cancel)
	defer stop()
	if flow.hostname != "" {
		ctx = context.WithValue(ctx, system.DeviceNameCtxKey, flow.hostname) //nolint:staticcheck // existing key type
	}

	state := internal.CtxGetState(s.rootCtx)
	state.Set(internal.StatusConnecting)
	err = internal.WaitPeerLoginMFA(ctx, deadline, func(ctx context.Context) error {
		return s.loginWithJWT(ctx, jwt)
	}, binding.managementURL, s.mfaPoll)
	switch {
	case err == nil:
		state.Set(internal.StatusIdle)
		return &proto.WaitMFAApprovalResponse{Email: email}, nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		state.Set(internal.StatusNeedsLogin)
		return nil, gstatus.Error(codes.Canceled, "stopped waiting for the 2FA approval")
	case errors.Is(err, internal.ErrPeerLoginMFAExpired):
		state.Set(internal.StatusNeedsLogin)
		return nil, gstatus.Error(codes.DeadlineExceeded, err.Error())
	default:
		state.Set(internal.StatusNeedsLogin)
		log.Warnf("device sign-in waiting for 2FA approval ended: %v", err)
		return nil, err
	}
}

// loginWithJWT logs in to the management server with an SSO token.
func (s *Server) loginWithJWT(ctx context.Context, jwt string) error {
	if s.peerLogin != nil {
		return s.peerLogin(ctx, jwt)
	}
	_, err := s.loginAttempt(ctx, "", jwt)
	return err
}

// mfaBinding is the profile and management server a flow belongs to.
func (s *Server) mfaBinding() mfaFlowBinding {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var mgmt string
	if s.config != nil && s.config.ManagementURL != nil {
		mgmt = s.config.ManagementURL.String()
	}
	return mfaFlowBinding{profile: s.activeProfileKey(), managementURL: mgmt}
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
