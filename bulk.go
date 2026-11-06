package emailvalidation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BulkService runs bulk jobs: client.Bulk.Create, Get, Start, Cancel, Wait, Results, ResultsStream and Download.
// Bulk calls need a key with the bulk scope.
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
	// File is a CSV file or one address per line (up to 100 MB): an *os.File, a *bytes.Reader, a
	// *strings.Reader or any other reader. It is read into memory once before sending, because the API needs a
	// known length and retries re-send the same bytes. The upload's timeout is an idle timeout (WithTimeout).
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
		cl.body, cl.contentType, cl.long = data, contentType, true
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
	// RowFilter selects the rows: []ResultsFilter{ResultsFilterAll}, one status or several
	// (FilterStatuses(StatusValid, StatusCatchAll), joined with commas). nil = server default (all). An empty list,
	// ResultsFilterAll inside a list, and anything that is not one of the five statuses (such as the retired
	// spamtrap, abuse, do_not_mail or risky) are refused before sending, with an error wrapping ErrInvalidArgument.
	// Set RowFilter or Filter, not both (both is refused the same way). Since 1.1.0.
	RowFilter []ResultsFilter
	// Filter is the 1.0.0 form, kept as it was: FilterAll, one status or several statuses (joined with commas).
	// nil or empty = server default (all). A name that is not FilterAll or one of the five statuses is refused
	// before sending. Prefer RowFilter, which also refuses an empty list and FilterAll inside a list.
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
	f, err := filterParam(o.RowFilter, o.Filter)
	if err != nil {
		return nil, err
	}
	setQuery(q, "filter", f)
	setQuery(q, "columns", string(o.Columns))
	if o.EscapeFormulas != nil {
		q.Set("escape_formulas", strconv.FormatBool(*o.EscapeFormulas))
	}
	return q, nil
}

// filterParam gives the filter query value ("" = none) of RowFilter or the 1.0.0 Filter.
func filterParam(rows []ResultsFilter, legacy []Status) (string, error) {
	switch {
	case rows != nil && legacy != nil:
		return "", argError("set RowFilter or Filter, not both")
	case rows != nil:
		return filterQuery(rows)
	}
	// The 1.0.0 rules, unchanged: empty means all rows, and only names outside "all" and the statuses are refused.
	parts := make([]string, len(legacy))
	for i, f := range legacy {
		if f != FilterAll && !IsStatus(string(f)) {
			return "", argError(fmt.Sprintf("unknown results filter %q", string(f)))
		}
		parts[i] = string(f)
	}
	return strings.Join(parts, ","), nil
}

// filterQuery checks a RowFilter: ResultsFilterAll alone, one status, or a non-empty list of statuses.
func filterQuery(filter []ResultsFilter) (string, error) {
	if len(filter) == 0 {
		return "", argError("the results filter is an empty list (leave it nil for all rows)")
	}
	parts := make([]string, len(filter))
	for i, f := range filter {
		switch {
		case f == ResultsFilterAll && len(filter) == 1:
		case f == ResultsFilterAll:
			return "", argError(`the results filter "all" cannot be combined with statuses`)
		case !IsStatus(string(f)):
			return "", argError(fmt.Sprintf("unknown results filter %q (use ResultsFilterAll or the five statuses)", string(f)))
		}
		parts[i] = string(f)
	}
	return strings.Join(parts, ","), nil
}

func rangeRefused(ans *answer) *Error {
	return &Error{
		Kind:      KindInvalidRequest,
		Status:    ans.status,
		Code:      CodeInvalidRequest,
		Message:   msgRangeRefused,
		RequestID: ans.header.Get("X-Request-Id"),
	}
}

// ResultsStream downloads the result CSV (UTF-8) of a completed job as a stream. Close it when done. Each read
// waits at most the client timeout (WithTimeout, or WithRequestTimeout) for data; no timer runs between reads, so the
// download is never cut while it progresses. Read errors are *Error (timeout, connection_error, or aborted when ctx
// is cancelled). With Range, an answer other than 206 is refused and its body closed unread.
func (b *BulkService) ResultsStream(ctx context.Context, jobID string, opts *ResultsOptions) (io.ReadCloser, error) {
	p, err := jobPath(jobID, "/results")
	if err != nil {
		return nil, err
	}
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	cl := &call{method: http.MethodGet, path: p, query: q, accept: "text/csv", long: true}
	if opts != nil {
		cl.rangeHeader = opts.Range
	}
	ans, err := b.c.send(ctx, cl, true)
	if err != nil {
		return nil, err
	}
	if cl.rangeHeader != "" && ans.status != http.StatusPartialContent {
		_ = ans.stream.Close()
		return nil, rangeRefused(ans)
	}
	return ans.stream, nil
}

// Results downloads the whole result CSV (UTF-8 bytes, unchanged), with the same idle timeout as ResultsStream.
// For large files prefer ResultsStream or Download.
func (b *BulkService) Results(ctx context.Context, jobID string, opts *ResultsOptions) ([]byte, error) {
	stream, err := b.ResultsStream(ctx, jobID, opts)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return io.ReadAll(stream) // read errors are already *Error
}

// DownloadOptions are the optional arguments of Download. The zero value downloads every row, resuming a partial
// file.
type DownloadOptions struct {
	// RowFilter, Columns and EscapeFormulas are as in ResultsOptions.
	RowFilter      []ResultsFilter
	Columns        ResultsColumns
	EscapeFormulas *bool
	// Resume continues a partial file at path (nil = true). Set Bool(false) to download again from the start.
	Resume *bool
}

// Download streams the result CSV of a completed job into the file at path (created if missing) and returns the
// file's size in bytes; the body is never held whole in memory. When the file already has bytes and Resume is not
// false, it asks only for the rest (Range: bytes=<size>-) and appends it, or returns at once when the file is
// already complete. Resuming needs a shape the API can serve in parts: RowFilter nil or ResultsFilterAll (or one status with
// EscapeFormulas false) and Columns original; another shape with a partial file is an error wrapping
// ErrInvalidArgument. A failure part-way leaves the partial file in place: call Download again to resume. Without a
// partial file the file is truncated only once the answer arrived. Every error is an *Error: a file that cannot be
// read, opened or written gives KindInvalidRequest (status 0) whose Err is the os error (errors.Is and errors.As
// reach it).
func (b *BulkService) Download(ctx context.Context, jobID, path string, opts *DownloadOptions) (int64, error) {
	p, err := jobPath(jobID, "/results")
	if err != nil {
		return 0, err
	}
	var o DownloadOptions
	if opts != nil {
		o = *opts
	}
	q, err := (&ResultsOptions{RowFilter: o.RowFilter, Columns: o.Columns, EscapeFormulas: o.EscapeFormulas}).query()
	if err != nil {
		return 0, err
	}
	var size int64
	if o.Resume == nil || *o.Resume {
		switch info, err := os.Stat(path); {
		case err == nil && info.IsDir():
			return 0, argError("the download path is a directory")
		case err == nil:
			size = info.Size()
		case !errors.Is(err, fs.ErrNotExist):
			return 0, fileError(err)
		}
	}
	cl := &call{method: http.MethodGet, path: p, query: q, accept: "text/csv", long: true}
	if size == 0 {
		ans, err := b.c.send(ctx, cl, true)
		if err != nil {
			return 0, err
		}
		defer ans.stream.Close()
		return writeBody(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0, ans.stream)
	}

	if !resumable(&o) {
		return 0, argError("this download cannot be resumed: use RowFilter all (or one status with EscapeFormulas " +
			"false) and Columns original, or Resume false")
	}
	cl.rangeHeader = "bytes=" + strconv.FormatInt(size, 10) + "-"
	cl.passStatus = http.StatusRequestedRangeNotSatisfiable
	ans, err := b.c.send(ctx, cl, true)
	if err != nil {
		return 0, err
	}
	defer ans.stream.Close()
	switch {
	case ans.status == http.StatusPartialContent:
		return writeBody(path, os.O_WRONLY, size, ans.stream)
	case ans.status == http.StatusRequestedRangeNotSatisfiable:
		if ans.header.Get("Content-Range") == "bytes */"+strconv.FormatInt(size, 10) {
			return size, nil // already complete
		}
		body, err := io.ReadAll(io.LimitReader(ans.stream, 64<<10))
		if err != nil {
			return 0, err
		}
		return 0, b.c.errorFromResponse(ans.status, ans.header, body)
	default:
		return 0, rangeRefused(ans) // the whole file: refused unread, the partial file is untouched
	}
}

// fileError is a failure to read, open or write the download file: an *Error (KindInvalidRequest, status 0, code
// invalid_request) whose Err is the os error, so errors.Is(err, fs.ErrPermission) and errors.As with *fs.PathError
// still work.
func fileError(err error) *Error {
	return &Error{Kind: KindInvalidRequest, Code: CodeInvalidRequest, Message: msgFileFailed, Err: err}
}

// resumable reports whether the API serves this results shape in parts.
func resumable(o *DownloadOptions) bool {
	if o.Columns != "" && o.Columns != ColumnsOriginal {
		return false
	}
	switch {
	case o.RowFilter == nil, len(o.RowFilter) == 1 && o.RowFilter[0] == ResultsFilterAll:
		return true
	case len(o.RowFilter) == 1:
		return o.EscapeFormulas != nil && !*o.EscapeFormulas
	}
	return false
}

// writeBody writes the body into the file at offset and returns the file's new size.
func writeBody(path string, flag int, offset int64, body io.Reader) (int64, error) {
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return 0, fileError(err)
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			_ = f.Close()
			return 0, fileError(err)
		}
	}
	n, err := io.Copy(onlyWriter{f}, body)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) {
			return 0, apiErr // a read failure: the partial file stays for the next call
		}
		return 0, fileError(err)
	}
	return offset + n, nil
}

// onlyWriter hides the file's ReadFrom, so io.Copy reads the body in bounded chunks through its Read.
type onlyWriter struct{ w io.Writer }

func (o onlyWriter) Write(p []byte) (int, error) { return o.w.Write(p) }

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
