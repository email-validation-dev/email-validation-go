package emailvalidation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Transport sends one HTTP attempt. The client builds the request, applies retries and maps errors; a Transport only
// moves bytes. Replace it (WithTransport) to use your own HTTP stack, a proxy library or a fake in tests.
//
// Do must honour ctx (it carries the per-attempt timeout and the caller's cancellation), must not follow redirects
// (return the 3xx answer instead) and returns either a *Response (any status) or an error when no HTTP answer
// arrived. Return a *TransportError to say whether that was a timeout.
type Transport interface {
	Do(ctx context.Context, req *Request) (*Response, error)
}

// Request is one HTTP attempt. Its string forms hide the headers and the body: they carry the API key and the
// checked address.
type Request struct {
	Method string
	// URL is absolute: base URL, path and encoded query.
	URL string
	// Header holds Authorization, Accept, User-Agent and, with a body, Content-Type (and Range for parts).
	Header http.Header `json:"-"`
	// Body is nil when the request has none. It is the same bytes on every retry.
	Body []byte `json:"-"`
	// Timeout is the client's per-attempt timeout (ctx already carries it as a deadline).
	Timeout time.Duration
}

// String shows the method and URL only.
func (r Request) String() string {
	return fmt.Sprintf("Request{%s %s, headers and body hidden}", r.Method, r.URL)
}

// GoString is the same as String, so %#v does not print the headers.
func (r Request) GoString() string { return r.String() }

// Format applies String to every verb, so no format verb prints the headers or the body.
func (r Request) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, r.String()) }

// Response is the answer to one attempt. The client closes Body.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

// TransportError reports that an attempt got no HTTP answer.
type TransportError struct {
	// Timeout is true when the attempt timed out (as opposed to a connection failure).
	Timeout bool
	Err     error
}

func (e *TransportError) Error() string {
	if e.Timeout {
		return "emailvalidation: transport: timeout"
	}
	if e.Err != nil {
		return "emailvalidation: transport: " + e.Err.Error()
	}
	return "emailvalidation: transport: connection failed"
}

// Unwrap returns the underlying error.
func (e *TransportError) Unwrap() error { return e.Err }

// HTTPTransport is the default Transport, on net/http. It never follows redirects.
type HTTPTransport struct {
	client *http.Client
}

// NewHTTPTransport wraps an *http.Client. A nil client uses a new one with the default transport. The client is
// copied so that its redirect policy can be set to "never follow" without changing yours; it is never closed.
func NewHTTPTransport(client *http.Client) *HTTPTransport {
	var c http.Client
	if client != nil {
		c = *client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPTransport{client: &c}
}

// Do sends the request with net/http.
func (t *HTTPTransport) Do(ctx context.Context, req *Request) (*Response, error) {
	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body) // known length: never a chunked upload
	}
	hr, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	hr.Header = req.Header.Clone()
	res, err := t.client.Do(hr)
	if err != nil {
		return nil, &TransportError{Timeout: isTimeout(err), Err: err}
	}
	return &Response{StatusCode: res.StatusCode, Header: res.Header, Body: res.Body}, nil
}

// closeIdleConnections releases idle connections of the wrapped client.
func (t *HTTPTransport) closeIdleConnections() { t.client.CloseIdleConnections() }

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var te *TransportError
	if errors.As(err, &te) && te.Timeout {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
