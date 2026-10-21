package internal

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/management/client/peerloginmfa"
)

// ErrPeerLoginMFAExpired ends a wait for a 2FA approval that did not come
// before the approval request expired.
var ErrPeerLoginMFAExpired = errors.New("the 2FA approval request expired; sign in again")

// PeerLoginMFAPoll sets how often a login waiting for its 2FA approval is
// retried: from Initial, growing by half each time up to Max, with ±20%
// jitter so many clients do not retry in step.
type PeerLoginMFAPoll struct {
	Initial time.Duration
	Max     time.Duration
}

// DefaultPeerLoginMFAPoll is the poll ADR-0028 D6 sets: 3 s up to 10 s.
var DefaultPeerLoginMFAPoll = PeerLoginMFAPoll{Initial: 3 * time.Second, Max: 10 * time.Second}

func (p PeerLoginMFAPoll) next(d time.Duration) time.Duration {
	return min(d+d/2, p.Max)
}

func (p PeerLoginMFAPoll) jitter(d time.Duration) time.Duration {
	// rand is fine here: the jitter only spreads load.
	return d*8/10 + time.Duration(rand.Int64N(int64(d*4/10)+1)) //nolint:gosec
}

// WaitPeerLoginMFA retries login while the management server answers that
// the device login waits for the user's 2FA approval (ADR-0028 D6). It
// returns nil once a login succeeds; the login's own error for a denial, a
// rate limit or a missing enrollment, which end the wait (callers read it
// with peerloginmfa.Parse, and a daemon can pass the gRPC status on as
// is); any other login error as is, except brief unavailability, which is
// retried;
// ErrPeerLoginMFAExpired at deadline; and ctx's error when it ends.
func WaitPeerLoginMFA(ctx context.Context, deadline time.Time, login func(context.Context) error,
	managementURL string, poll PeerLoginMFAPoll) error {
	if !time.Now().Before(deadline) {
		return ErrPeerLoginMFAExpired
	}
	// The deadline bounds the login calls too, so a stalled call cannot
	// keep the wait, and the token it holds, past the approval's life.
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ended := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrPeerLoginMFAExpired
	}

	wait := poll.Initial
	for {
		err := login(waitCtx)
		if err == nil {
			return nil
		}
		if waitCtx.Err() != nil {
			return ended()
		}
		if refusal, ok := peerloginmfa.Parse(err, managementURL); ok {
			if refusal.Reason != peerloginmfa.ReasonRequired {
				return err
			}
		} else if !transient(err) {
			return err
		}

		sleep := poll.jitter(wait)
		if time.Until(deadline) < sleep {
			return ErrPeerLoginMFAExpired
		}
		select {
		case <-waitCtx.Done():
			return ended()
		case <-time.After(sleep):
		}
		wait = poll.next(wait)
	}
}

func transient(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}
