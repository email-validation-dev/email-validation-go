package emailvalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Client calls the email-validation.org REST API. Create it once with New and share it: it is safe for concurrent
// use. Every method takes a context.Context first; cancelling it stops the call (and any retry or poll wait) at once
// with a ConnectionError whose Code is CodeAborted (errors.Is(err, context.Canceled) also holds).
type Client struct {
	// Bulk runs bulk jobs.
	Bulk *BulkService

	apiKey       string
	baseURL      string
	timeout      time.Duration
	maxRetries   int
	maxRetryWait time.Duration
	transport    Transport
	ownsHTTP     *HTTPTransport
	userAgent    string
	sleep        func(ctx context.Context, d time.Duration) error
	now          func() time.Time
}

// Option configures a Client.
type Option func(*settings)

type settings struct {
	baseURL      string
	timeout      time.Duration
	maxRetries   int
	maxRetryWait time.Duration
	transport    Transport
	httpClient   *http.Client
	sleep        func(ctx context.Context, d time.Duration) error
	now          func() time.Time
}

// WithBaseURL sets the API base URL (default DefaultBaseURL). It must start with http:// or https://; trailing
// slashes are removed.
func WithBaseURL(baseURL string) Option { return func(s *settings) { s.baseURL = baseURL } }

// WithTimeout sets the timeout of each attempt, covering connect and the full response (default 30 s; must be
// > 0). A check may take up to 15 s on our side: keep it a few seconds above the Validate TimeoutSeconds.
func WithTimeout(d time.Duration) Option { return func(s *settings) { s.timeout = d } }

// WithMaxRetries sets how many times a call is retried after the first attempt (default 2; negative means 0).
func WithMaxRetries(n int) Option { return func(s *settings) { s.maxRetries = n } }

// WithMaxRetryWait sets the longest single wait the client accepts before a retry (default 10 s; negative
// means 0). A longer Retry-After ends the call with the error instead.
func WithMaxRetryWait(d time.Duration) Option { return func(s *settings) { s.maxRetryWait = d } }

// WithHTTPClient sends requests with your *http.Client (proxies, custom TLS). The client is copied so that
// redirects are never followed; yours is not modified or closed.
func WithHTTPClient(c *http.Client) Option { return func(s *settings) { s.httpClient = c } }

// WithTransport replaces the HTTP stack (see Transport). It takes precedence over WithHTTPClient.
func WithTransport(t Transport) Option { return func(s *settings) { s.transport = t } }

// New creates a client. apiKey is your secret API key (ev_live_…): keep it on the server. It returns an error
// wrapping ErrInvalidArgument for an empty key, a key with whitespace, a base URL that is not http(s) or a
// timeout that is not positive.
func New(apiKey string, opts ...Option) (*Client, error) {
	s := settings{
		baseURL:      DefaultBaseURL,
		timeout:      DefaultTimeout,
		maxRetries:   DefaultMaxRetries,
		maxRetryWait: DefaultMaxRetryWait,
	}
	for _, o := range opts {
		if o != nil {
			o(&s)
		}
	}
	if !validKey(apiKey) {
		return nil, argError("an API key is required (no whitespace)")
	}
	base := strings.TrimRight(s.baseURL, "/")
	if !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://") {
		return nil, argError("the base URL must start with http:// or https://")
	}
	if s.timeout <= 0 {
		return nil, argError("the timeout must be positive")
	}
	c := &Client{
		apiKey:       apiKey,
		baseURL:      base,
		timeout:      s.timeout,
		maxRetries:   max(0, s.maxRetries),
		maxRetryWait: max(0, s.maxRetryWait),
		transport:    s.transport,
		userAgent:    "email-validation-go/" + Version + " (" + runtime.Version() + ")",
		sleep:        s.sleep,
		now:          s.now,
	}
	if c.transport == nil {
		t := NewHTTPTransport(s.httpClient)
		c.transport = t
		if s.httpClient == nil {
			c.ownsHTTP = t
		}
	}
	if c.sleep == nil {
		c.sleep = sleepContext
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.Bulk = &BulkService{c: c}
	return c, nil
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		if unicode.IsSpace(r) || r == 0xFEFF {
			return false
		}
	}
	return true
}

// Close releases idle connections of the HTTP client the SDK created. It never closes a client or transport you
// supplied. The Client must not be used afterwards.
func (c *Client) Close() error {
	if c.ownsHTTP != nil {
		c.ownsHTTP.closeIdleConnections()
	}
	return nil
}

// String shows the base URL only: never the API key.
func (c Client) String() string {
	return fmt.Sprintf("emailvalidation.Client{baseURL: %q, apiKey: [redacted]}", c.baseURL)
}

// GoString is the same as String.
func (c Client) GoString() string { return c.String() }

// Format applies String to every verb, so no format verb prints the API key.
func (c Client) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

// Bool returns a pointer to b, for optional fields such as ValidateOptions.Fresh.
func Bool(b bool) *bool { return &b }

// ValidateOptions are the optional arguments of Validate. The zero value sends none.
type ValidateOptions struct {
	// IPAddress is the IP address the person used when they gave you the address.
	IPAddress string
	// TimeoutSeconds is how long the service may spend on the check: 5–15 seconds (0 = server default, 15).
	// Keep the client timeout (WithTimeout) a few seconds above it.
	TimeoutSeconds int
	// Fresh set to true checks the address again even when it was checked in the last 12 months (by default such
	// a check's definite result may be returned; ProcessedAt is when the answer was produced). nil = server
	// default (false).
	Fresh *bool
}

// Validate checks one address. It costs 1 credit unless the result is unknown or answered locally (free). The
// address is sent as is, in a POST body. Sandbox addresses (SandboxAddress) are free.
//
// A Validate that got no answer is not retried, because the check may already have been charged; 429 and 503
// answers (never charged) are retried.
func (c *Client) Validate(ctx context.Context, email string, opts *ValidateOptions) (*ValidationResult, error) {
	body := map[string]any{"email": email}
	if opts != nil {
		if opts.IPAddress != "" {
			body["ip_address"] = opts.IPAddress
		}
		if opts.TimeoutSeconds != 0 {
			body["timeout"] = opts.TimeoutSeconds
		}
		if opts.Fresh != nil {
			body["fresh"] = *opts.Fresh
		}
	}
	data, err := encodeJSON(body)
	if err != nil {
		return nil, err
	}
	var out ValidationResult
	err = c.doJSON(ctx, &call{method: http.MethodPost, path: "/v1/validate", body: data, contentType: "application/json"}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Credits returns the balance, debt, pending credits and credit lots.
func (c *Client) Credits(ctx context.Context) (*Credits, error) {
	var out Credits
	if err := c.doJSON(ctx, &call{method: http.MethodGet, path: "/v1/credits"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UsageParams are the optional arguments of Usage. The zero value sends none.
type UsageParams struct {
	// From and To are dates as YYYY-MM-DD (UTC).
	From string
	To   string
	// GroupBy is GroupByDay (server default) or GroupByKey.
	GroupBy UsageGroupBy
}

// Usage returns credit usage by day (default) or by API key.
func (c *Client) Usage(ctx context.Context, params *UsageParams) (*UsageResponse, error) {
	q := url.Values{}
	if params != nil {
		setQuery(q, "from", params.From)
		setQuery(q, "to", params.To)
		setQuery(q, "group_by", string(params.GroupBy))
	}
	var out UsageResponse
	if err := c.doJSON(ctx, &call{method: http.MethodGet, path: "/v1/usage", query: q}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Status returns the public service status (no credits).
func (c *Client) Status(ctx context.Context) (*ServiceStatus, error) {
	var out ServiceStatus
	if err := c.doJSON(ctx, &call{method: http.MethodGet, path: "/v1/status"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- plumbing ----

type call struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
	accept      string
	rangeHeader string
}

// answer is a successful (2xx) response: body holds the whole body, or stream is open (and must be closed).
type answer struct {
	status int
	header http.Header
	body   []byte
	stream io.ReadCloser
}

func setQuery(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (c *Client) doJSON(ctx context.Context, cl *call, out any) error {
	ans, err := c.send(ctx, cl, false)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(ans.body, out); err != nil {
		return &Error{
			Kind:      KindEmailValidation,
			Status:    ans.status,
			Code:      CodeUnexpectedResponse,
			Message:   msgNotJSON,
			RequestID: ans.header.Get("X-Request-Id"),
			Err:       err,
		}
	}
	return nil
}

func (c *Client) buildRequest(cl *call) *Request {
	u := c.baseURL + cl.path
	if len(cl.query) > 0 {
		u += "?" + cl.query.Encode()
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.apiKey)
	accept := cl.accept
	if accept == "" {
		accept = "application/json"
	}
	h.Set("Accept", accept)
	h.Set("User-Agent", c.userAgent)
	if cl.body != nil && cl.contentType != "" {
		h.Set("Content-Type", cl.contentType)
	}
	if cl.rangeHeader != "" {
		h.Set("Range", cl.rangeHeader)
	}
	return &Request{Method: cl.method, URL: u, Header: h, Body: cl.body, Timeout: c.timeout}
}

// send runs the retry loop. With stream, a 2xx answer's body is returned open; the per-attempt timeout keeps
// running until it is closed.
func (c *Client) send(ctx context.Context, cl *call, stream bool) (*answer, error) {
	req := c.buildRequest(cl)
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, abortedError(err)
		}
		actx, cancel := context.WithTimeout(ctx, c.timeout)
		attemptReq := *req
		attemptReq.Header = req.Header.Clone()
		res, err := c.transport.Do(actx, &attemptReq)
		if err != nil && res != nil && res.Body != nil {
			_ = res.Body.Close() // a misbehaving transport returned both: never leak the body
		}
		var body []byte
		if err == nil && res == nil {
			err = &TransportError{Err: fmt.Errorf("transport returned no response")}
		}
		if err == nil {
			if stream && res.StatusCode >= 200 && res.StatusCode < 300 {
				return &answer{
					status: res.StatusCode,
					header: res.Header,
					stream: &cancelOnClose{ReadCloser: bodyOrEmpty(res.Body), cancel: cancel},
				}, nil
			}
			body, err = readAll(res.Body)
		}
		cancel()

		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, abortedError(ctxErr)
			}
			timedOut := actx.Err() == context.DeadlineExceeded || isTimeout(err)
			if cl.method == http.MethodGet && attempt < c.maxRetries {
				if err := c.pause(ctx, backoff(attempt)); err != nil {
					return nil, err
				}
				continue
			}
			if timedOut {
				return nil, &Error{Kind: KindConnection, Code: CodeTimeout, Message: msgTimedOut, Err: err}
			}
			return nil, &Error{Kind: KindConnection, Code: CodeConnectionError, Message: msgUnreachable, Err: err}
		}

		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return &answer{status: res.StatusCode, header: res.Header, body: body}, nil
		}
		apiErr := c.errorFromResponse(res.StatusCode, res.Header, body)
		if (res.StatusCode == 429 || res.StatusCode == 503) && apiErr.Code != CodeKeyDailyCap && attempt < c.maxRetries {
			wait := backoff(attempt)
			accept := wait <= c.maxRetryWait
			if apiErr.RetryAfter != nil {
				// Compared in seconds first: a huge Retry-After must not overflow time.Duration.
				accept = float64(*apiErr.RetryAfter) <= c.maxRetryWait.Seconds()
				wait = time.Duration(*apiErr.RetryAfter) * time.Second
			}
			if accept {
				if err := c.pause(ctx, wait); err != nil {
					return nil, err
				}
				continue
			}
		}
		return nil, apiErr
	}
}

func bodyOrEmpty(b io.ReadCloser) io.ReadCloser {
	if b == nil {
		return io.NopCloser(bytes.NewReader(nil))
	}
	return b
}

func readAll(b io.ReadCloser) ([]byte, error) {
	if b == nil {
		return []byte{}, nil
	}
	defer b.Close()
	data, err := io.ReadAll(b)
	if err != nil {
		return nil, &TransportError{Timeout: isTimeout(err), Err: err}
	}
	return data, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func abortedError(cause error) *Error {
	return &Error{Kind: KindConnection, Code: CodeAborted, Message: msgAborted, Err: cause}
}

func (c *Client) pause(ctx context.Context, d time.Duration) error {
	if err := c.sleep(ctx, d); err != nil {
		cause := ctx.Err()
		if cause == nil {
			cause = err
		}
		return abortedError(cause)
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// backoff is exponential with jitter: attempt 0 waits 0.25–0.5 s, 1 waits 0.5–1 s, 2 waits 1–2 s, capped at 4–8 s.
func backoff(attempt int) time.Duration {
	base := math.Min(8, 0.5*math.Pow(2, float64(attempt)))
	secs := base/2 + rand.Float64()*base/2
	return time.Duration(secs * float64(time.Second))
}

func (c *Client) errorFromResponse(status int, header http.Header, body []byte) *Error {
	e := &Error{
		Kind:       kindForStatus(status),
		Status:     status,
		RequestID:  header.Get("X-Request-Id"),
		RetryAfter: parseRetryAfter(header.Get("Retry-After"), c.now()),
	}
	var envelope struct {
		Error *struct {
			Code      any `json:"code"`
			Message   any `json:"message"`
			RequestID any `json:"request_id"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		if code, ok := envelope.Error.Code.(string); ok && isAPIErrorCode(code) {
			e.Code = ErrorCode(code)
			e.Message = code
			if msg, ok := envelope.Error.Message.(string); ok {
				e.Message = msg
			}
			if rid, ok := envelope.Error.RequestID.(string); ok {
				e.RequestID = rid
			}
			return e
		}
	}
	e.Code = CodeUnexpectedResponse
	e.Message = "Unexpected HTTP " + strconv.Itoa(status) + " from the API."
	return e
}

// parseRetryAfter reads Retry-After: digits are seconds; an HTTP date gives max(0, ceil(date - now)); anything
// else gives nil.
func parseRetryAfter(value string, now time.Time) *int {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	if isDigits(v) {
		n, err := strconv.Atoi(v)
		if err != nil {
			// Too many digits for an int: still a (very long) wait, which the client never accepts. Returning nil
			// here would fall back to a short backoff and retry instead.
			n = math.MaxInt
		}
		return &n
	}
	at, err := http.ParseTime(v)
	if err != nil {
		return nil
	}
	secs := int(math.Ceil(at.Sub(now).Seconds()))
	secs = max(0, secs)
	return &secs
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
