package emailvalidation

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BulkService runs bulk jobs: client.Bulk.Create, Get, Start, Cancel, Results, ResultsStream and Wait. Bulk calls
// need a key with the bulk scope.
type BulkService struct {
	c *Client
}

// String hides the client's internals.
func (b *BulkService) String() string { return "emailvalidation.BulkService{}" }

var jobIDPattern = regexp.MustCompile(`\Ajob_[0-9A-Za-z]{22}\z`)

func jobPath(id, suffix string) (string, error) {
	if !jobIDPattern.MatchString(id) {
		return "", &Error{Kind: KindInvalidRequest, Code: CodeInvalidRequest, Message: msgBadJobID}
	}
	return "/v1/bulk/" + id + suffix, nil
}

// BulkCreateParams describes a new job: exactly one of Emails or File, plus options.
type BulkCreateParams struct {
	// Emails is a list of addresses, sent as JSON (up to 100,000 addresses / 4 MB). A non-nil empty slice counts
	// as given.
	Emails []string
	// File is a CSV file or one address per line (up to 100 MB). It is read into memory before sending, because
	// the API needs a known length and retries re-send the same bytes.
	File io.Reader
	// ContentType of File: ContentTypeCSV (default) or ContentTypeText.
	ContentType string
	// ColumnIndex is the 1-based index of the address column (0 = detect). Set at most one of ColumnIndex and
	// ColumnName.
	ColumnIndex int
	// ColumnName is the header name of the address column ("" = detect).
	ColumnName string
	// HasHeader says whether the first row is a header (nil = detect).
	HasHeader *bool
	// Start set to false keeps the job in ready after analysis until you call Start (nil = server default, true).
	Start *bool
	// WebhookEndpointID is a registered webhook endpoint to notify when the job ends.
	WebhookEndpointID string
}

// Create creates a job from a list or a file and returns it (status analyzing). With Emails it sends JSON even for
// one address.
func (b *BulkService) Create(ctx context.Context, params *BulkCreateParams) (*BulkJob, error) {
	if params == nil || (params.Emails == nil) == (params.File == nil) {
		return nil, argError("pass exactly one of Emails or File")
	}
	contentType := params.ContentType
	if contentType == "" {
		contentType = ContentTypeCSV
	}
	if contentType != ContentTypeCSV && contentType != ContentTypeText {
		return nil, argError("ContentType must be text/csv or text/plain")
	}
	if params.ColumnIndex != 0 && params.ColumnName != "" {
		return nil, argError("set at most one of ColumnIndex and ColumnName")
	}
	q := url.Values{}
	if params.ColumnIndex != 0 {
		q.Set("column", strconv.Itoa(params.ColumnIndex))
	}
	setQuery(q, "column", params.ColumnName)
	if params.HasHeader != nil {
		q.Set("has_header", strconv.FormatBool(*params.HasHeader))
	}
	if params.Start != nil {
		q.Set("start", strconv.FormatBool(*params.Start))
	}
	setQuery(q, "webhook_endpoint_id", params.WebhookEndpointID)

	cl := &call{method: http.MethodPost, path: "/v1/bulk", query: q}
	if params.Emails != nil {
		data, err := encodeJSON(map[string][]string{"emails": params.Emails})
		if err != nil {
			return nil, err
		}
		cl.body, cl.contentType = data, "application/json"
	} else {
		data, err := io.ReadAll(params.File)
		if err != nil {
			return nil, fmt.Errorf("emailvalidation: reading File: %w", err)
		}
		if data == nil {
			data = []byte{}
		}
		cl.body, cl.contentType = data, contentType
	}
	return b.job(ctx, cl)
}

func (b *BulkService) job(ctx context.Context, cl *call) (*BulkJob, error) {
	var out BulkJob
	if err := b.c.doJSON(ctx, cl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Get returns a job. A malformed id fails before any request (KindInvalidRequest, status 0).
func (b *BulkService) Get(ctx context.Context, jobID string) (*BulkJob, error) {
	p, err := jobPath(jobID, "")
	if err != nil {
		return nil, err
	}
	return b.job(ctx, &call{method: http.MethodGet, path: p})
}

// Start starts a job in ready; credits are held now.
func (b *BulkService) Start(ctx context.Context, jobID string) (*BulkJob, error) {
	p, err := jobPath(jobID, "/start")
	if err != nil {
		return nil, err
	}
	return b.job(ctx, &call{method: http.MethodPost, path: p})
}

// Cancel cancels a job; unused credits are returned.
func (b *BulkService) Cancel(ctx context.Context, jobID string) (*BulkJob, error) {
	p, err := jobPath(jobID, "")
	if err != nil {
		return nil, err
	}
	return b.job(ctx, &call{method: http.MethodDelete, path: p})
}

// ResultsOptions are the optional arguments of Results and ResultsStream. The zero value sends none.
type ResultsOptions struct {
	// Filter is FilterAll, one status or several statuses (joined with commas). nil = server default (all).
	Filter []Status
	// Columns is ColumnsOriginal (server default: every column of your file) or ColumnsEmailOnly.
	Columns ResultsColumns
	// EscapeFormulas neutralises spreadsheet formulas in your own cells (nil = server default, true).
	EscapeFormulas *bool
	// Range is a byte range of the full file, e.g. "bytes=0-1048575", to resume a download. Use Filter all (or
	// one status with EscapeFormulas false) and Columns original: other shapes cannot be served in parts and fail
	// with KindInvalidRequest instead of returning the whole file, so appended parts never duplicate data.
	Range string
}

func (o *ResultsOptions) query() (url.Values, error) {
	q := url.Values{}
	if o == nil {
		return q, nil
	}
	if len(o.Filter) > 0 {
		parts := make([]string, len(o.Filter))
		for i, f := range o.Filter {
			if f != FilterAll && !IsStatus(string(f)) {
				return nil, argError(fmt.Sprintf("unknown results filter %q", string(f)))
			}
			parts[i] = string(f)
		}
		q.Set("filter", strings.Join(parts, ","))
	}
	setQuery(q, "columns", string(o.Columns))
	if o.EscapeFormulas != nil {
		q.Set("escape_formulas", strconv.FormatBool(*o.EscapeFormulas))
	}
	return q, nil
}

// ResultsStream downloads the result CSV (UTF-8) of a completed job as a stream. Close it when done. The client
// timeout (WithTimeout) covers the whole download: raise it for large files.
func (b *BulkService) ResultsStream(ctx context.Context, jobID string, opts *ResultsOptions) (io.ReadCloser, error) {
	p, err := jobPath(jobID, "/results")
	if err != nil {
		return nil, err
	}
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	cl := &call{method: http.MethodGet, path: p, query: q, accept: "text/csv"}
	if opts != nil {
		cl.rangeHeader = opts.Range
	}
	ans, err := b.c.send(ctx, cl, true)
	if err != nil {
		return nil, err
	}
	if cl.rangeHeader != "" && ans.status != http.StatusPartialContent {
		_ = ans.stream.Close()
		return nil, &Error{
			Kind:      KindInvalidRequest,
			Status:    ans.status,
			Code:      CodeInvalidRequest,
			Message:   msgRangeRefused,
			RequestID: ans.header.Get("X-Request-Id"),
		}
	}
	return ans.stream, nil
}

// Results downloads the whole result CSV (UTF-8 bytes, unchanged). For large files prefer ResultsStream.
func (b *BulkService) Results(ctx context.Context, jobID string, opts *ResultsOptions) ([]byte, error) {
	stream, err := b.ResultsStream(ctx, jobID, opts)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, abortedError(ctxErr)
		}
		if isTimeout(err) {
			return nil, &Error{Kind: KindConnection, Code: CodeTimeout, Message: msgTimedOut, Err: err}
		}
		return nil, &Error{Kind: KindConnection, Code: CodeConnectionError, Message: msgUnreachable, Err: err}
	}
	return data, nil
}

// WaitOptions are the optional arguments of Wait.
type WaitOptions struct {
	// Interval between polls (0 = 5 s; at least 1 s).
	Interval time.Duration
	// Timeout gives up after this long with a ConnectionError, Code CodeTimeout (0 = no limit). The context's
	// deadline also ends the wait (Code CodeAborted).
	Timeout time.Duration
	// OnProgress is called with every polled job.
	OnProgress func(job *BulkJob)
}

// Wait polls a job and returns it once it is completed, failed, cancelled or expired, or once it is ready with an
// ErrorCode (it could not start, for example insufficient_credits: buy credits, then call Start). Check job.Status
// before downloading results. A job created with Start false waits in ready until you call Start, so call Start
// before Wait. Each poll follows the normal retry rules.
func (b *BulkService) Wait(ctx context.Context, jobID string, opts *WaitOptions) (*BulkJob, error) {
	if _, err := jobPath(jobID, ""); err != nil {
		return nil, err
	}
	var o WaitOptions
	if opts != nil {
		o = *opts
	}
	interval := o.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	interval = max(time.Second, interval)
	var deadline time.Time
	if o.Timeout > 0 {
		deadline = b.c.now().Add(o.Timeout)
	}
	for {
		job, err := b.Get(ctx, jobID)
		if err != nil {
			return nil, err
		}
		if o.OnProgress != nil {
			o.OnProgress(job)
		}
		if job.IsTerminal() || job.isStuckReady() {
			return job, nil
		}
		if !deadline.IsZero() && b.c.now().Add(interval).After(deadline) {
			return nil, &Error{
				Kind:    KindConnection,
				Code:    CodeTimeout,
				Message: fmt.Sprintf("Bulk job %s is still %s.", jobID, job.Status),
			}
		}
		if err := b.c.pause(ctx, interval); err != nil {
			return nil, err
		}
	}
}
