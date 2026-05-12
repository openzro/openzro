package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/openzro/openzro/flow/proto"
	"github.com/openzro/openzro/util/embeddedroots"
	nbgrpc "github.com/openzro/openzro/util/grpc"
)

// ErrClientClosed is the permanent error returned from Receive when
// Close lands during the retry loop. Wrapped in backoff.Permanent so
// the loop unwinds immediately instead of waiting out the backoff.
var ErrClientClosed = errors.New("flow client: client is closed")

// minHealthyDuration is the minimum time a stream must survive
// before a transport failure counts as "the stream worked, the
// network just blipped" — we reset the backoff timer in that
// case. Streams that die faster than this are considered unhealthy
// (TLS handshake failures, server returning UNAVAILABLE immediately,
// auth header rejection) and must NOT reset backoff, so that
// MaxElapsedTime can eventually stop the retry loop.
const minHealthyDuration = 5 * time.Second

type GRPCClient struct {
	realClient proto.FlowServiceClient
	clientConn *grpc.ClientConn
	stream     proto.FlowService_EventsClient
	// target + opts are remembered from NewClient so the retry loop
	// can rebuild the underlying grpc.ClientConn from scratch when
	// the existing one enters a stuck state — gRPC's internal
	// subchannel backoff can otherwise outlive our own retry timer.
	target string
	opts   []grpc.DialOption
	// closed becomes true after Close so concurrent retries
	// terminate instead of dialing a new conn on the dead client.
	closed bool
	// receiving guards against two Receive goroutines racing on the
	// same client. The flag is checked under streamMu.
	receiving bool
	streamMu  sync.Mutex
}

func NewClient(addr, payload, signature string, interval time.Duration) (*GRPCClient, error) {
	parsedURL, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("parsing url: %w", err)
	}
	var opts []grpc.DialOption
	if parsedURL.Scheme == "https" {
		certPool, err := x509.SystemCertPool()
		if err != nil || certPool == nil {
			log.Debugf("System cert pool not available; falling back to embedded cert, error: %v", err)
			certPool = embeddedroots.Get()
		}

		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			RootCAs: certPool,
		})))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	opts = append(opts,
		nbgrpc.WithCustomDialer(),
		grpc.WithIdleTimeout(interval*2),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		withAuthToken(payload, signature),
		grpc.WithDefaultServiceConfig(`{"healthCheckConfig": {"serviceName": ""}}`),
	)

	target := fmt.Sprintf("%s:%s", parsedURL.Hostname(), parsedURL.Port())
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating new grpc client: %w", err)
	}

	return &GRPCClient{
		realClient: proto.NewFlowServiceClient(conn),
		clientConn: conn,
		target:     target,
		opts:       opts,
	}, nil
}

// Close marks the client as closed and tears down the underlying
// ClientConn. Any concurrent Receive loop observes the closed flag
// the next time it tries to recreate the connection and exits with
// ErrClientClosed instead of dialing again.
func (c *GRPCClient) Close() error {
	c.streamMu.Lock()
	c.closed = true
	c.stream = nil
	conn := c.clientConn
	c.clientConn = nil
	c.streamMu.Unlock()

	if conn == nil {
		return nil
	}
	if err := conn.Close(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("close client connection: %w", err)
	}
	return nil
}

// ErrConcurrentReceive is returned when a second goroutine tries to
// call Receive on a client that is already serving one. Callers
// should treat this as a programming error — there is no legitimate
// path that calls Receive twice in parallel on the same client.
var ErrConcurrentReceive = errors.New("flow client: concurrent Receive calls are not supported")

func (c *GRPCClient) Receive(ctx context.Context, interval time.Duration, msgHandler func(msg *proto.FlowEventAck) error) error {
	c.streamMu.Lock()
	if c.receiving {
		c.streamMu.Unlock()
		return ErrConcurrentReceive
	}
	c.receiving = true
	c.streamMu.Unlock()
	defer func() {
		c.streamMu.Lock()
		c.receiving = false
		c.streamMu.Unlock()
	}()

	backOff := defaultBackoff(ctx, interval)
	operation := func() error {
		stream, err := c.establishStream(ctx)
		if err != nil {
			log.Errorf("failed to establish flow stream, retrying: %v", err)
			return c.handleRetryableError(err, time.Time{}, backOff)
		}

		streamStart := time.Now()

		if err := c.receive(stream, msgHandler); err != nil {
			log.Errorf("receive failed: %v", err)
			return c.handleRetryableError(err, streamStart, backOff)
		}
		return nil
	}

	if err := backoff.Retry(operation, backOff); err != nil {
		return fmt.Errorf("receive failed permanently: %w", err)
	}

	return nil
}

// handleRetryableError decides whether to retry the loop. Returns a
// backoff.Permanent error when the client was closed or the context
// is done, otherwise rebuilds the ClientConn (gRPC's internal
// subchannel backoff can outlive our own and leave the conn in a
// permanently-unavailable state without our cooperation) and returns
// a retryable error. A stream that survived `minHealthyDuration`
// resets the backoff timer so a brief outage after hours of healthy
// operation doesn't count against MaxElapsedTime.
func (c *GRPCClient) handleRetryableError(err error, streamStart time.Time, backOff backoff.BackOff) error {
	if isContextDone(err) {
		return backoff.Permanent(err)
	}

	var permErr *backoff.PermanentError
	if errors.As(err, &permErr) {
		return err
	}

	if !streamStart.IsZero() && time.Since(streamStart) >= minHealthyDuration {
		backOff.Reset()
	}

	if recreateErr := c.recreateConnection(); recreateErr != nil {
		log.Errorf("recreate connection: %v", recreateErr)
		return recreateErr
	}

	log.Infof("connection recreated, retrying stream")
	return fmt.Errorf("retrying after error: %w", err)
}

// recreateConnection swaps the underlying ClientConn for a fresh one
// built from the same target + opts. Old conn is closed outside the
// lock to avoid blocking concurrent callers (Close races are safe
// because Close itself takes streamMu, sets closed=true, and nils
// clientConn before unlocking).
func (c *GRPCClient) recreateConnection() error {
	c.streamMu.Lock()
	if c.closed {
		c.streamMu.Unlock()
		return backoff.Permanent(ErrClientClosed)
	}

	conn, err := grpc.NewClient(c.target, c.opts...)
	if err != nil {
		c.streamMu.Unlock()
		return fmt.Errorf("create new connection: %w", err)
	}

	old := c.clientConn
	c.clientConn = conn
	c.realClient = proto.NewFlowServiceClient(conn)
	c.stream = nil
	c.streamMu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	return nil
}

// establishStream opens an Events stream, sends the initiator
// message, reads the headers, and publishes the stream pointer
// under streamMu so Send can race-safely read it. The blocking
// Events() call is made outside the lock to keep other paths
// responsive while the dial completes.
func (c *GRPCClient) establishStream(ctx context.Context) (proto.FlowService_EventsClient, error) {
	c.streamMu.Lock()
	if c.closed {
		c.streamMu.Unlock()
		return nil, backoff.Permanent(ErrClientClosed)
	}
	cl := c.realClient
	c.streamMu.Unlock()

	stream, err := cl.Events(ctx)
	if err != nil {
		return nil, fmt.Errorf("create event stream: %w", err)
	}
	streamReady := false
	defer func() {
		if !streamReady {
			_ = stream.CloseSend()
		}
	}()

	if err := stream.Send(&proto.FlowEvent{IsInitiator: true}); err != nil {
		return nil, fmt.Errorf("send initiator: %w", err)
	}

	if err := checkHeader(stream); err != nil {
		return nil, fmt.Errorf("check header: %w", err)
	}

	c.streamMu.Lock()
	if c.closed {
		c.streamMu.Unlock()
		return nil, backoff.Permanent(ErrClientClosed)
	}
	c.stream = stream
	c.streamMu.Unlock()
	streamReady = true

	return stream, nil
}

// isContextDone reports whether the local context has been cancelled
// or has exceeded its deadline. We deliberately do NOT inspect gRPC
// status codes: a server- or proxy-sent codes.Canceled /
// DeadlineExceeded must not short-circuit our retry loop, since
// retrying is the correct response when the local context is alive.
func isContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (c *GRPCClient) receive(stream proto.FlowService_EventsClient, msgHandler func(msg *proto.FlowEventAck) error) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			// Return the raw error so handleRetryableError can
			// distinguish context cancellation from transport failure
			// without unwrapping a wrapped status.
			return err
		}

		if msg.IsInitiator {
			log.Tracef("received initiator message from flow receiver")
			continue
		}

		if err := msgHandler(msg); err != nil {
			return fmt.Errorf("handle message: %w", err)
		}
	}
}

func checkHeader(stream proto.FlowService_EventsClient) error {
	header, err := stream.Header()
	if err != nil {
		log.Errorf("waiting for flow receiver header: %s", err)
		return fmt.Errorf("wait for header: %w", err)
	}

	if len(header) == 0 {
		log.Error("flow receiver sent no headers")
		return fmt.Errorf("should have headers")
	}
	return nil
}

func defaultBackoff(ctx context.Context, interval time.Duration) backoff.BackOff {
	return backoff.WithContext(&backoff.ExponentialBackOff{
		InitialInterval:     800 * time.Millisecond,
		RandomizationFactor: 1,
		Multiplier:          1.7,
		MaxInterval:         interval / 2,
		MaxElapsedTime:      3 * 30 * 24 * time.Hour, // 3 months
		Stop:                backoff.Stop,
		Clock:               backoff.SystemClock,
	}, ctx)
}

func (c *GRPCClient) Send(event *proto.FlowEvent) error {
	c.streamMu.Lock()
	stream := c.stream
	c.streamMu.Unlock()

	if stream == nil {
		return errors.New("stream not initialized")
	}

	if err := stream.Send(event); err != nil {
		return fmt.Errorf("send flow event: %w", err)
	}

	return nil
}
