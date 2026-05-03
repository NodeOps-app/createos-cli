package telemetry

import (
	"context"
	"sync"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/NodeOps-app/createos-cli/internal/config"
)

// Default is the package-global Client. Written once by Init. Other packages
// MUST tolerate Default == nil (call sites use the nil-receiver no-op
// pattern).
var Default *Client

// initOnce guards Init so re-entry is a cheap no-op.
var initOnce sync.Once

// Client wraps the posthog-go client with our distinct_id / user_id state.
//
// Thread-safety: posthog-go's Enqueue is itself goroutine-safe. The mutex
// here only guards the userID/distinctID swap inside RebindIdentity so that
// a concurrent Capture sees a consistent pair.
type Client struct {
	inner         posthog.Client
	machineIDHash string // anonymous distinct_id; never overwritten after Init
	globalProps   posthog.Properties

	mu          sync.Mutex
	userID      string // empty pre-login
	distinctID  string // == machineIDHash pre-login, == userID post-login
	disabled    bool
	personProps map[string]any // sent on Identify only; never on Capture events
}

// silentLogger satisfies posthog.Logger but drops all output. Telemetry must
// never write to stderr/stdout.
type silentLogger struct{}

func (silentLogger) Logf(string, ...interface{})   {}
func (silentLogger) Errorf(string, ...interface{}) {}
func (silentLogger) Debugf(string, ...interface{}) {}
func (silentLogger) Warnf(string, ...interface{})  {}

// Init constructs Default. Idempotent — second call is a no-op.
//
// On disabled (no key, or CREATEOS_DO_NOT_TRACK=1), Default is set to a
// non-nil sentinel client whose methods are all no-ops. Callers can deref
// without nil-checks, but the package-global is still nil-checked for the
// belt-and-braces case where Init was never reached at all.
func Init(ctx context.Context) error {
	var initErr error
	initOnce.Do(func() {
		if IsDisabled() {
			Default = &Client{disabled: true}
			return
		}

		inner, err := posthog.NewWithConfig(effectiveKey(), posthog.Config{
			Endpoint:  effectiveHost(),
			BatchSize: 20,
			Interval:  5 * time.Second,
			Logger:    silentLogger{},
		})
		if err != nil {
			// Fail-soft: degrade to disabled rather than surfacing a
			// telemetry error to the user.
			Default = &Client{disabled: true}
			initErr = err
			return
		}

		c := &Client{
			inner:         inner,
			machineIDHash: ResolveDistinctID(),
			globalProps:   GlobalProperties(ctx),
		}
		c.distinctID = c.machineIDHash
		Default = c

		// Apply any persisted identity (alias + identify + switch distinctID).
		c.RebindIdentity()
	})
	return initErr
}

// Capture enqueues a custom event. Safe no-op when c is nil or disabled.
func (c *Client) Capture(event string, props map[string]any) {
	if c == nil || c.disabled || c.inner == nil {
		return
	}

	merged := posthog.NewProperties()
	for k, v := range c.globalProps {
		merged[k] = v
	}
	for k, v := range props {
		merged[k] = v
	}

	c.mu.Lock()
	distinctID := c.distinctID
	userID := c.userID
	c.mu.Unlock()

	if userID != "" {
		merged["user_id"] = userID
	}

	if distinctID == "" {
		// posthog-go validates DistinctId is non-empty; drop silently.
		return
	}

	_ = c.inner.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      event,
		Properties: merged,
	})
}

// SetPersonProperties stores PostHog Person-level properties (email, name,
// signup_date, ...) that will be attached to the next Identify event sent
// by RebindIdentity. The map is held in memory only — it is never persisted
// to disk and never appears on Capture event payloads.
//
// Callers (the login flow) MUST call this BEFORE RebindIdentity for the
// props to land on the corresponding Identify.
func (c *Client) SetPersonProperties(props map[string]any) {
	if c == nil || c.disabled {
		return
	}
	cp := make(map[string]any, len(props))
	for k, v := range props {
		cp[k] = v
	}
	c.mu.Lock()
	c.personProps = cp
	c.mu.Unlock()
}

// RebindIdentity reads the on-disk Identity and aligns the client state.
// Idempotent — repeated calls do nothing extra once user is bound.
//
// CRITICAL: alias is ALWAYS emitted from machineIDHash, never from the
// current distinctID (which may already point to a previous user_id from
// this same process — aliasing user→user collapses the wrong nodes).
func (c *Client) RebindIdentity() {
	if c == nil || c.disabled || c.inner == nil {
		return
	}
	id, err := config.LoadIdentity()
	if err != nil || id == nil || id.UserID == "" {
		return
	}

	if id.AliasedForUserID != id.UserID {
		_ = c.inner.Enqueue(posthog.Alias{
			DistinctId: c.machineIDHash,
			Alias:      id.UserID,
		})
		id.AliasedForUserID = id.UserID
		_ = config.SaveIdentity(*id)
	}

	identifyProps := posthog.NewProperties()
	if v, ok := c.globalProps["is_ci"]; ok {
		identifyProps.Set("is_ci", v)
	}
	if v, ok := c.globalProps["ci_provider"]; ok {
		identifyProps.Set("ci_provider", v)
	}
	if v, ok := c.globalProps["channel"]; ok {
		identifyProps.Set("channel", v)
	}
	// Attach Person props (email, name, signup_date, ...) supplied by the
	// login flow. They go ONLY to PostHog's person record via Identify; they
	// are NOT included in any Capture event payload.
	c.mu.Lock()
	personProps := c.personProps
	c.mu.Unlock()
	setOnce := map[string]any{}
	for k, v := range personProps {
		switch k {
		case "signup_date", "created_at":
			// Immutable Person fields — only set on first identify per
			// person, never overwritten on subsequent logins.
			setOnce[k] = v
		default:
			identifyProps.Set(k, v)
		}
	}
	if len(setOnce) > 0 {
		identifyProps.Set("$set_once", setOnce)
	}
	_ = c.inner.Enqueue(posthog.Identify{
		DistinctId: id.UserID,
		Properties: identifyProps,
	})

	c.mu.Lock()
	c.userID = id.UserID
	c.distinctID = id.UserID
	c.mu.Unlock()
}

// Shutdown best-effort flushes pending events.
//
// posthog-go has no non-terminal Flush primitive; Close() is the only way to
// drain the batch and it is TERMINAL — further Enqueue calls return errors.
// We bound the wait on the caller's ctx (typically a 500ms deadline) and
// disable the client locally so subsequent Capture calls are cheap no-ops.
func (c *Client) Shutdown(ctx context.Context) {
	if c == nil || c.disabled || c.inner == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = c.inner.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	c.mu.Lock()
	c.disabled = true
	c.mu.Unlock()
}
