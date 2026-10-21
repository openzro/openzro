package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	gstatus "google.golang.org/grpc/status"
)

const (
	// maxPendingMFAFlows bounds the logins the daemon keeps waiting for a
	// 2FA approval; adding one more drops the oldest (ADR-0028 D6).
	maxPendingMFAFlows = 4
	// mfaFlowReapInterval is how often abandoned flows are dropped.
	mfaFlowReapInterval = 30 * time.Second
)

// mfaFlowBinding is what a flow belongs to: it can only be waited on while
// the daemon still uses the same profile and management server.
type mfaFlowBinding struct {
	profile       string
	managementURL string
}

// mfaFlow is a device login that waits for the user's 2FA approval. It
// holds the SSO token the login needs, in memory only, until the flow ends.
type mfaFlow struct {
	id        string
	binding   mfaFlowBinding
	jwt       string
	email     string
	hostname  string
	expiresAt time.Time
	createdAt time.Time
	waiting   bool
}

// mfaFlows holds the pending flows, keyed by an unguessable id. A flow is
// single-use and has at most one waiter, so a UI and a CLI, or two
// attempts, cannot drive the same login.
type mfaFlows struct {
	mu    sync.Mutex
	flows map[string]*mfaFlow
}

func newMFAFlows() *mfaFlows {
	return &mfaFlows{flows: make(map[string]*mfaFlow)}
}

// add stores a flow and returns its id, dropping the oldest flow that is
// not being waited on when the set is full.
func (f *mfaFlows) add(binding mfaFlowBinding, jwt, email, hostname string, expiresAt, now time.Time) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate flow id: %w", err)
	}
	id := hex.EncodeToString(raw)

	f.mu.Lock()
	defer f.mu.Unlock()
	for len(f.flows) >= maxPendingMFAFlows {
		var oldest *mfaFlow
		for _, flow := range f.flows {
			if !flow.waiting && (oldest == nil || flow.createdAt.Before(oldest.createdAt)) {
				oldest = flow
			}
		}
		if oldest == nil {
			return "", gstatus.Error(codes.ResourceExhausted, "too many sign-ins are waiting for 2FA approval")
		}
		f.dropLocked(oldest.id)
	}
	f.flows[id] = &mfaFlow{
		id: id, binding: binding, jwt: jwt, email: email, hostname: hostname, expiresAt: expiresAt, createdAt: now,
	}
	return id, nil
}

// claim hands the flow to its one waiter.
func (f *mfaFlows) claim(id string, binding mfaFlowBinding, now time.Time) (*mfaFlow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	flow, ok := f.flows[id]
	switch {
	case !ok:
		return nil, gstatus.Error(codes.NotFound, "no sign-in is waiting for 2FA approval with this id")
	case flow.waiting:
		return nil, gstatus.Error(codes.AlreadyExists, "this sign-in is already being waited on")
	case !now.Before(flow.expiresAt):
		f.dropLocked(id)
		return nil, gstatus.Error(codes.DeadlineExceeded, "the 2FA approval request expired; sign in again")
	case flow.binding != binding:
		f.dropLocked(id)
		return nil, gstatus.Error(codes.FailedPrecondition, "the profile or management server changed; sign in again")
	}
	flow.waiting = true
	return flow, nil
}

// finish ends a flow and drops its token.
func (f *mfaFlows) finish(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropLocked(id)
}

// reap drops expired flows that nobody waits on; a waiter ends its own.
func (f *mfaFlows) reap(now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, flow := range f.flows {
		if !flow.waiting && !now.Before(flow.expiresAt) {
			f.dropLocked(id)
		}
	}
}

func (f *mfaFlows) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.flows)
}

func (f *mfaFlows) dropLocked(id string) {
	if flow, ok := f.flows[id]; ok {
		flow.jwt = ""
		delete(f.flows, id)
	}
}
