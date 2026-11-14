package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Computer-use routes: GET/POST /v1/sandboxes/:id/computer/*.
//
// These drive the X session inside a sandbox booted on a desktop rootfs —
// screenshot, pointer, keyboard, window list — plus the noVNC connect URL a
// human opens in a browser. Every call is scoped to one screen, selected by
// the screen_id query parameter.
//
// Screenshot is the only route that answers with bytes (image/png) instead of
// the JSend envelope, so it bypasses SetResult and reads resp.Body() directly.

// DefaultComputerScreen is the screen every computer call targets unless the
// caller names another one.
const DefaultComputerScreen = "screen-0"

// ComputerScreenGeometry is GET /computer/screen — the coordinate space every
// other call's x/y is measured in. Raw X11 pixels: no DPI scaling is applied
// anywhere along this path, so callers must read the bounds rather than assume
// a resolution.
type ComputerScreenGeometry struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ComputerCursorPos is GET /computer/cursor.
type ComputerCursorPos struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ComputerConnection is GET /computer/screens/:screen/connect — a live noVNC
// URL with a bearer token embedded in it, plus that token's expiry.
//
// The URL *is* the credential: anyone holding it can drive the desktop until
// it expires. fc only mints one when ingress is enabled on the sandbox.
type ComputerConnection struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

// ComputerScreen returns the screen's pixel geometry. It doubles as the
// readiness probe: it is the cheapest route that only answers 2xx once the
// desktop stack (Xvfb → XFCE → x11vnc → websockify) is actually up.
func (c *SandboxClient) ComputerScreen(ctx context.Context, id, screen string) (*ComputerScreenGeometry, error) {
	var envelope Response[ComputerScreenGeometry]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetQueryParam("screen_id", computerScreen(screen)).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}/computer/screen")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseComputerError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ComputerCursor returns the pointer's current position.
func (c *SandboxClient) ComputerCursor(ctx context.Context, id, screen string) (*ComputerCursorPos, error) {
	var envelope Response[ComputerCursorPos]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetQueryParam("screen_id", computerScreen(screen)).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}/computer/cursor")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseComputerError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ComputerWindows lists the windows on the screen. The window shape is fc's to
// define and has no stable schema here yet, so the payload is passed through
// unparsed rather than pinned to a struct this repo would have to guess at.
func (c *SandboxClient) ComputerWindows(ctx context.Context, id, screen string) (json.RawMessage, error) {
	return c.computerGetRaw(ctx, id, screen, "/v1/sandboxes/{id}/computer/windows")
}

// ComputerScreenshot captures the screen and returns the PNG bytes verbatim.
//
// This route answers image/png, not the JSend envelope, so an error body has
// to be read off the same response — hence the manual IsError branch before
// the bytes are handed back.
func (c *SandboxClient) ComputerScreenshot(ctx context.Context, id, screen string) ([]byte, error) {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetQueryParam("screen_id", computerScreen(screen)).
		SetHeader("Accept", "image/png").
		Get("/v1/sandboxes/{id}/computer/screenshot")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseComputerError(resp.StatusCode(), resp.Body())
	}
	return resp.Body(), nil
}

// ComputerMouseMove moves the pointer to an absolute position on the screen.
func (c *SandboxClient) ComputerMouseMove(ctx context.Context, id, screen string, x, y int) error {
	return c.computerPost(ctx, id, screen, "/v1/sandboxes/{id}/computer/mouse/move",
		map[string]int{"x": x, "y": y})
}

// ComputerMouseClick clicks. With at == nil it clicks wherever the pointer
// already is; otherwise it moves there first.
func (c *SandboxClient) ComputerMouseClick(ctx context.Context, id, screen string, at *ComputerCursorPos) error {
	body := map[string]int{}
	if at != nil {
		body["x"] = at.X
		body["y"] = at.Y
	}
	return c.computerPost(ctx, id, screen, "/v1/sandboxes/{id}/computer/mouse/click", body)
}

// ComputerType types a string into the focused window.
func (c *SandboxClient) ComputerType(ctx context.Context, id, screen, text string) error {
	return c.computerPost(ctx, id, screen, "/v1/sandboxes/{id}/computer/keyboard/type",
		map[string]string{"text": text})
}

// ComputerPress presses one key chord — each element is a key name, and they
// are pressed together (e.g. ["ctrl","l"]).
func (c *SandboxClient) ComputerPress(ctx context.Context, id, screen string, keys []string) error {
	return c.computerPost(ctx, id, screen, "/v1/sandboxes/{id}/computer/keyboard/press",
		map[string][]string{"keys": keys})
}

// ComputerOpen opens a URL or a local path in the desktop's browser.
func (c *SandboxClient) ComputerOpen(ctx context.Context, id, screen, target string) error {
	return c.computerPost(ctx, id, screen, "/v1/sandboxes/{id}/computer/open",
		map[string]string{"target": target})
}

// ComputerConnect mints a noVNC URL for the screen. fc only returns one when
// ingress is enabled on the sandbox; a fresh call invalidates the previous
// link for new connections.
func (c *SandboxClient) ComputerConnect(ctx context.Context, id, screen string) (*ComputerConnection, error) {
	var envelope Response[ComputerConnection]
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetPathParam("screen", computerScreen(screen)).
		SetResult(&envelope).
		Get("/v1/sandboxes/{id}/computer/screens/{screen}/connect")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseComputerError(resp.StatusCode(), resp.Body())
	}
	return &envelope.Data, nil
}

// ComputerRaw is the escape hatch for the computer routes this client does not
// wrap. path is relative to /v1/sandboxes/:id/computer (a leading slash makes
// it absolute instead). body may be nil.
func (c *SandboxClient) ComputerRaw(ctx context.Context, id, screen, method, path string, body json.RawMessage) (json.RawMessage, error) {
	full := path
	if !strings.HasPrefix(path, "/") {
		full = fmt.Sprintf("/v1/sandboxes/%s/computer/%s", id, path)
	}
	req := c.Client.R().
		SetContext(ctx).
		SetQueryParam("screen_id", computerScreen(screen))
	if len(body) > 0 {
		req = req.SetBody(body)
	}
	resp, err := req.Execute(strings.ToUpper(method), full)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseComputerError(resp.StatusCode(), resp.Body())
	}
	return unwrapEnvelope(resp.Body()), nil
}

// computerPost is the shared shape of every action route: POST a small JSON
// body, care only about success or failure.
func (c *SandboxClient) computerPost(ctx context.Context, id, screen, path string, body any) error {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetQueryParam("screen_id", computerScreen(screen)).
		SetBody(body).
		Post(path)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseComputerError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// computerGetRaw fetches a route whose payload this client deliberately does
// not model, and returns it unwrapped from the JSend envelope.
func (c *SandboxClient) computerGetRaw(ctx context.Context, id, screen, path string) (json.RawMessage, error) {
	resp, err := c.Client.R().
		SetContext(ctx).
		SetPathParam("id", id).
		SetQueryParam("screen_id", computerScreen(screen)).
		Get(path)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseComputerError(resp.StatusCode(), resp.Body())
	}
	return unwrapEnvelope(resp.Body()), nil
}

// computerScreen applies the default so callers can pass "".
func computerScreen(screen string) string {
	if strings.TrimSpace(screen) == "" {
		return DefaultComputerScreen
	}
	return screen
}

// unwrapEnvelope pulls `data` out of a JSend body, falling back to the whole
// body when the response is not enveloped.
func unwrapEnvelope(body []byte) json.RawMessage {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		return envelope.Data
	}
	return body
}

// ComputerError is a failed computer-use call. It keeps the status code so
// callers that poll (readiness waits) can tell a "not up yet" apart from a
// "this will never work", instead of retrying until the timeout either way.
type ComputerError struct {
	StatusCode int
	Message    string
	advice     string
}

func (e *ComputerError) Error() string {
	if e.advice == "" {
		return e.Message
	}
	return e.Message + "\n\n" + e.advice
}

// Retryable reports whether polling the same route again could plausibly
// succeed. A 409 is the interesting case: fc uses it both for "the desktop is
// still coming up" and "the action failed on a live desktop", so a waiter has
// to keep trying while an action caller should surface it.
func (e *ComputerError) Retryable() bool {
	switch e.StatusCode {
	case http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

// ParseComputerError maps the computer API's status codes onto something a
// caller can act on.
//
// This is worth doing by hand rather than leaning on ParseAPIError, because
// fc returns `desktop_unavailable` (409) for *every* X-side failure — the raw
// message alone never distinguishes "the desktop is still booting" from "the
// action failed on a perfectly healthy desktop", and those want opposite
// responses from whoever is reading the error.
func ParseComputerError(statusCode int, body []byte) error {
	msg := ""
	if base := ParseAPIError(statusCode, body); base != nil {
		msg = base.Message
	}
	e := &ComputerError{StatusCode: statusCode, Message: msg}

	switch statusCode {
	case http.StatusBadRequest:
		e.Message = "the desktop couldn't use that" + suffix(msg)
		e.advice = "  If you named a screen, check it exists. Most sandboxes have only\n  screen-0, which is the default.\n  To see the screen you have, run:\n    createos sandbox computer screen <sandbox>"
	case http.StatusNotFound:
		e.Message = "that sandbox or screen doesn't exist" + suffix(msg)
		e.advice = "  Computer-use needs a sandbox booted on a desktop image.\n  To start one, run:\n    createos sandbox create --rootfs desktop:1"
	case http.StatusConflict:
		if strings.Contains(strings.ToLower(msg), "ingress") {
			e.Message = "the public URL is off for this sandbox" + suffix(msg)
			e.advice = "  To turn it on, run:\n    createos sandbox desktop <sandbox>"
			break
		}
		e.Message = "the desktop didn't answer" + suffix(msg)
		e.advice = "  Either the desktop is still starting up, or the action failed on a\n  running desktop — the API reports both the same way.\n  If the sandbox just started, wait for it with:\n    createos sandbox desktop <sandbox>"
	case http.StatusTooManyRequests:
		e.Message = "too many requests in a row" + suffix(msg)
		e.advice = "  Screenshots are rate limited. Wait a second and try again."
	case http.StatusNotImplemented:
		e.Message = "this sandbox has no desktop installed" + suffix(msg)
		e.advice = "  Its image doesn't include the desktop tools. Create a new one with:\n    createos sandbox create --rootfs desktop:1"
	default:
		if e.Message == "" {
			e.Message = fmt.Sprintf("the desktop request failed (HTTP %d)", statusCode)
		}
	}
	return e
}

// suffix formats an optional server message as a trailing clause.
func suffix(msg string) string {
	if msg == "" {
		return ""
	}
	return ": " + msg
}
