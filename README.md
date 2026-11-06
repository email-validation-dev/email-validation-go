# email-validation.org Go SDK

Official Go client for the [email-validation.org](https://email-validation.org) API: validate single addresses, read
credits, usage and service status, run bulk jobs (upload, wait, results, download with resume) and verify webhook
signatures. Standard library only (`net/http`, `encoding/json`, `crypto/hmac`); no dependencies. Needs Go 1.22 or
later; tested on Go 1.27.

Documentation: https://email-validation.org/docs/sdks/go/

## Install

```bash
go get email-validation.org/go/emailvalidation
```

```go
import "email-validation.org/go/emailvalidation"
```

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"email-validation.org/go/emailvalidation"
)

func main() {
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	result, err := client.Validate(context.Background(), "accepted@sandbox.email-validation.org", nil) // free test address
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Status, result.Reason, result.Action) // valid accepted send
}
```

`result.Status` is one of five: `valid`, `invalid`, `catch_all`, `unknown` or `disposable`; `Reason` explains it and
`Action` says what to do. Create the client once and share it: it is safe for concurrent use. Keep the API key on the
server. `<reason>@sandbox.email-validation.org` addresses are free and answer that reason
(`emailvalidation.SandboxAddress(emailvalidation.ReasonCatchAll)`).

## Client options

```go
client, err := emailvalidation.New(apiKey,
	emailvalidation.WithBaseURL(emailvalidation.DefaultBaseURL), // or emailvalidation.EUBaseURL
	emailvalidation.WithTimeout(30*time.Second),                 // per attempt (default 30 s)
	emailvalidation.WithMaxRetries(2),                           // retries after the first attempt (default 2)
	emailvalidation.WithMaxRetryWait(10*time.Second),            // longest accepted wait before a retry (default 10 s)
	emailvalidation.WithHTTPClient(myHTTPClient),                // optional: your *http.Client
)
if err != nil {
	log.Fatal(err)
}
defer client.Close()
```

- `apiKey`: your secret key (`ev_live_…`), required, no whitespace.
- `WithBaseURL`: default `https://api.email-validation.org`. For EU data residency (validated and stored in the EU)
  pass `emailvalidation.EUBaseURL`, `https://api-eu.email-validation.org`; see
  https://email-validation.org/docs/eu-data-residency/.
- `WithTimeout` (default 30 s, must be > 0) bounds each attempt. For JSON calls it covers the whole attempt: connect,
  send and the full answer. For transfers (`Bulk.Create` with a `File`, `Bulk.Results`, `Bulk.ResultsStream`,
  `Bulk.Download`) it is an idle timeout: the attempt fails only when no byte was sent or received for that long, so a
  slow but progressing 100 MB upload or download is never cut.
- Request timeout: `emailvalidation.WithRequestTimeout(ctx, d)` replaces `WithTimeout` for the calls made with that
  context, with the same rules; when it passes the error has code `timeout`.
- `WithMaxRetries` (default 2, negative means 0) and `WithMaxRetryWait` (default 10 s): see
  [Retries, timeouts and cancellation](#retries-timeouts-and-cancellation).
- Transport: `WithHTTPClient` or `WithTransport`, see [Bring your own HTTP client](#bring-your-own-http-client).

`New` returns an error wrapping `emailvalidation.ErrInvalidArgument` for an empty key, a key containing whitespace,
a base URL that does not start with `http://` or `https://`, or a timeout that is not positive. `client.Close()`
releases idle connections of the HTTP client the SDK created (never one you supplied).

## Validate an address

```go
result, err := client.Validate(ctx, "accepted@sandbox.email-validation.org", &emailvalidation.ValidateOptions{
	IPAddress:      "203.0.113.7",              // the IP the person used when they gave you the address
	TimeoutSeconds: 10,                         // time the service may spend: 5–15 s (default 15)
	Fresh:          emailvalidation.Bool(true), // check again even if checked in the last 12 months
})
if err != nil {
	log.Fatal(err)
}
switch result.Action {
case emailvalidation.ActionSend, emailvalidation.ActionSendWithCaution:
	fmt.Println("keep", result.Email)
case emailvalidation.ActionRemove:
	fmt.Println("remove", result.Email)
case emailvalidation.ActionRetryLater:
	fmt.Println("check again later", result.Email)
}
```

Every method takes a `context.Context` first; optional arguments are an options struct pointer (`nil` sends none).
A check costs 1 credit unless the result is `unknown` or answered locally (`result.CreditsUsed` is `0` or `1`). Spam
traps and frequent complainers are `valid` with the action `remove` (reason `spamtrap` / `complainer`). Nullable
fields are pointers (`nil` = JSON `null`), and times are the API's exact ISO 8601 UTC strings. A status, reason,
action or provider that this SDK version does not know yet is kept as its raw string, so decoding never fails on a
new value.

## Credits, usage and status

```go
credits, err := client.Credits(ctx) // Balance, Debt, Pending, Lots (soonest-expiring first)
if err != nil {
	log.Fatal(err)
}
usage, err := client.Usage(ctx, &emailvalidation.UsageParams{
	From: "2026-10-01", To: "2026-10-20", // YYYY-MM-DD, UTC
	GroupBy: emailvalidation.GroupByKey,  // GroupByDay (default) fills usage.Days, GroupByKey fills usage.Keys
})
if err != nil {
	log.Fatal(err)
}
status, err := client.Status(ctx) // Status and Components (Website, API, Bulk, SignIn, Payments, Support)
if err != nil {
	log.Fatal(err)
}
fmt.Println(credits.Balance, len(usage.Keys), status.Status)
```

## Bulk jobs

```go
// from a list (sent as JSON, up to 100,000 addresses)
job, err := client.Bulk.Create(ctx, &emailvalidation.BulkCreateParams{
	Emails: []string{"accepted@sandbox.email-validation.org", "no_mailbox@sandbox.email-validation.org"},
})
if err != nil {
	log.Fatal(err)
}

// or from a file (CSV, or one address per line with ContentType: emailvalidation.ContentTypeText), up to 100 MB:
// any io.Reader works, an *os.File, a *bytes.Reader, a *strings.Reader or a stream
f, err := os.Open("contacts.csv")
if err != nil {
	log.Fatal(err)
}
defer f.Close()
job, err = client.Bulk.Create(ctx, &emailvalidation.BulkCreateParams{
	File: f, ColumnName: "email", HasHeader: emailvalidation.Bool(true),
	Start: emailvalidation.Bool(false), // start it yourself below
})
if err != nil {
	log.Fatal(err)
}
if _, err := client.Bulk.Start(ctx, job.ID); err != nil { // call Start before Wait
	log.Fatal(err)
}

job, err = client.Bulk.Wait(ctx, job.ID, &emailvalidation.WaitOptions{
	Interval:   5 * time.Second,  // default 5 s, at least 1 s
	Timeout:    30 * time.Minute, // default: no limit
	OnProgress: func(j *emailvalidation.BulkJob) { log.Println(j.Status, j.Processed) },
})
if err != nil {
	log.Fatal(err)
}
if job.Status != emailvalidation.BulkJobStatusCompleted {
	log.Fatalf("job ended %s (%v)", job.Status, job.ErrorCode)
}

// the whole CSV in memory
csv, err := client.Bulk.Results(ctx, job.ID, &emailvalidation.ResultsOptions{
	RowFilter: emailvalidation.FilterStatuses(emailvalidation.StatusValid, emailvalidation.StatusCatchAll),
})
if err != nil {
	log.Fatal(err)
}
fmt.Print(string(csv))

// or as a stream (close it)
stream, err := client.Bulk.ResultsStream(ctx, job.ID, nil)
if err != nil {
	log.Fatal(err)
}
_, err = io.Copy(os.Stdout, stream)
stream.Close()
if err != nil {
	log.Fatal(err)
}

// or straight into a file: a second call after a failure resumes where the file ends
size, err := client.Bulk.Download(ctx, job.ID, "results.csv", nil)
if err != nil {
	log.Fatal(err) // call Download again to resume
}
fmt.Println(size, "bytes")
```

- `Create` takes exactly one of `Emails` or `File`. A `File` is read into memory once and sent with its length (the
  API refuses chunked uploads; a retry re-sends the same bytes). Options: `ColumnIndex` (1-based) or `ColumnName`,
  `HasHeader`, `Start` and `WebhookEndpointID`. Bulk calls need a key with the `bulk` scope.
- `Wait` returns the job once it is `completed`, `failed`, `cancelled` or `expired`, or once it is `ready` with an
  `ErrorCode` (its start was refused, e.g. `insufficient_credits`: buy credits, then call `Start`). A job created
  with `Start: emailvalidation.Bool(false)` stays in `ready` until you call `Start`, so call `Start` before `Wait`.
  When `Timeout` passes, `Wait` returns a `ConnectionError` with code `timeout`.
- Results: `RowFilter` is `[]emailvalidation.ResultsFilter{emailvalidation.ResultsFilterAll}`, one status or
  several (`FilterStatuses(...)`); an empty list, `ResultsFilterAll` inside a list and names that are not one of the
  five statuses (the retired `spamtrap`, `abuse`, `do_not_mail`, `risky`) are refused before sending. The 1.0.0 field
  `Filter []Status` (with `FilterAll`) still works as it did, an empty list meaning every row; set one of the two, not
  both. `Columns` is `ColumnsOriginal` (default) or `ColumnsEmailOnly`; `EscapeFormulas` defaults to true on the
  server.
- `Range` (e.g. `"bytes=0-1048575"`) fetches one part of the file. The API serves parts only for every row (or
  one status with `EscapeFormulas: emailvalidation.Bool(false)`) and `ColumnsOriginal`; other shapes return an
  `InvalidRequestError` (and the body is closed unread) instead of the whole file.
- `Download(ctx, jobID, path, *DownloadOptions)` streams the CSV into the file and returns its size. When the file
  already has bytes it asks only for the rest (`Range: bytes=<size>-`) and appends it, or returns at once when the
  file is complete; `Resume: emailvalidation.Bool(false)` starts again. Resuming needs the resumable shape above. A
  failure part-way keeps the partial file for the next call. A file that cannot be opened or written is an
  `InvalidRequestError` (status 0) whose `Err` is the os error (`errors.Is(err, fs.ErrPermission)` works).
- A malformed job id is refused before any request (`InvalidRequestError`, status 0, code `invalid_request`).

## Handle errors

```go
_, err := client.Validate(ctx, address, nil)
var apiErr *emailvalidation.Error
switch {
case err == nil:
	fmt.Println("ok")
case errors.Is(err, emailvalidation.ErrInsufficientCredits):
	// buy credits (nothing was charged)
case errors.Is(err, emailvalidation.ErrRateLimited):
	if errors.As(err, &apiErr) && apiErr.RetryAfter != nil {
		log.Printf("retry in %d s", *apiErr.RetryAfter)
	}
case errors.As(err, &apiErr):
	log.Println(apiErr.Kind, apiErr.Status, apiErr.Code, apiErr.RequestID)
}
```

Every API call returns an `*emailvalidation.Error`. Branch on its kind (`errors.Is` with a sentinel, or `Kind`) or on
its `Code`, never on the message. Fields: `Kind`, `Status` (HTTP status, `0` when no answer arrived), `Code`,
`Message` (for people; may change), `RequestID` (quote it to support; `""` when absent), `RetryAfter` (`*int`
seconds) and `Err` (the cause, for `errors.Unwrap`). Its string form shows the kind, status, code, request id and
message.

| Kind (sentinel) | When | Codes |
|---|---|---|
| `KindInvalidRequest` (`ErrInvalidRequest`) | 400, 411, 413, other 4xx, refused before sending (status 0), range answered without 206 | `invalid_request`, `length_required`, `payload_too_large` |
| `KindAuthentication` (`ErrAuthentication`) | 401 | `unauthorized` |
| `KindInsufficientCredits` (`ErrInsufficientCredits`) | 402 (nothing charged) | `insufficient_credits` |
| `KindPermissionDenied` (`ErrPermissionDenied`) | 403 | `forbidden`, `forbidden_origin` |
| `KindNotFound` (`ErrNotFound`) | 404 | `not_found` |
| `KindConflict` (`ErrConflict`) | 409 | `conflict` |
| `KindRateLimit` (`ErrRateLimited`) | 429 | `rate_limited`, `abuse_throttled`, `key_daily_cap` |
| `KindServiceUnavailable` (`ErrServiceUnavailable`) | 503 | `service_unavailable`, `upstream_capacity` |
| `KindServer` (`ErrServer`) | 500, other 5xx, any other non-2xx (e.g. a redirect, which is never followed) | `internal_error` |
| `KindConnection` (`ErrConnection`) | no answer, timed out, cancelled, `Bulk.Wait` deadline | `connection_error`, `timeout`, `aborted` |
| `KindEmailValidation` (`ErrUnexpectedResponse`) | a 2xx answer that is not JSON | `unexpected_response` |

An answer without our error envelope (or with an unknown code) gets code `unexpected_response` and the message
`Unexpected HTTP <status> from the API.` Wrong arguments (both or neither of `Emails` and `File`, an unknown
`ContentType` or results filter, a request timeout that is not positive) return an error wrapping
`emailvalidation.ErrInvalidArgument` before any request. Errors while reading a results stream are `*Error` too.

## Retries, timeouts and cancellation

- `429` and `503` answers are never charged, so they are retried for every method, `POST` included, up to
  `WithMaxRetries` times (default 2). The wait is `Retry-After` (seconds or an HTTP date) when the answer has one,
  otherwise exponential backoff with jitter (0.25–0.5 s, then 0.5–1 s, 1–2 s, … capped at 4–8 s). A wait longer
  than `WithMaxRetryWait` (default 10 s) ends the call with the error instead. `key_daily_cap` is never retried (it
  resets the next day).
- A request that got no answer (network failure or timeout) is retried only for `GET`. A `Validate`, bulk upload,
  start or cancel is never sent again after no answer, because it may already have been charged or applied; the SDK
  is the only place a request is repeated (`net/http` does not resend a `POST` or `DELETE` by itself, and a test
  proves it). A results stream or download is never retried once a byte reached you; `Download` resumes on the next
  call.
- Nothing else is retried (400–428, 500, 502, 504, …). Retries re-send the same bytes.
- Timeouts: `WithTimeout` (default 30 s) bounds each attempt, not the whole call: the whole attempt for JSON calls,
  an idle timeout for transfers (no byte for that long, during the upload, while waiting for the answer and during
  each read; no timer runs while you are busy between two reads of a stream). The SDK enforces both itself, so a
  transport that ignores the context cannot hang a call. `WithRequestTimeout(ctx, d)` changes the timeout for one
  call. The kernel buffers part of an upload before it leaves the machine, so while the upload runs and while the
  answer is awaited after its last byte the idle bound is `timeout + min(size, 4 MiB) ÷ 128 KiB/s` (at most 32 s
  more). The `TimeoutSeconds` of `Validate` (5–15 s) is the time the service may spend; keep the client timeout a few
  seconds above it.
- Cancellation: cancelling the context (or its own deadline passing) stops the call at once, during a request, a
  retry or poll wait, or a stream read, with a `ConnectionError` of code `aborted`; `errors.Is(err, context.Canceled)`
  (or `context.DeadlineExceeded`) also holds. A cancelled call is never retried.

## Verify webhooks

```go
http.HandleFunc("/webhooks/email-validation", func(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body) // the raw body, before any JSON parsing
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	event, err := emailvalidation.VerifyWebhook(body, r.Header, os.Getenv("EV_WEBHOOK_SECRET"), nil)
	if err != nil {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	// Deduplicate on r.Header.Get("webhook-id"): retries reuse it.
	switch event.Type {
	case emailvalidation.WebhookBulkCompleted, emailvalidation.WebhookBulkFailed:
		data, err := event.BulkData() // JobID, Status, Processed, CreditsCharged, Counts, ErrorCode
		if err == nil {
			log.Println(data.JobID, data.Status)
		}
	case emailvalidation.WebhookCreditsLow: // event.CreditsLowData(): Balance, Threshold
	case emailvalidation.WebhookCreditsExpiring: // event.CreditsExpiringData(): Credits, ExpiresAt
	case emailvalidation.WebhookTest: // "Send test" in the dashboard
	}
	w.WriteHeader(http.StatusNoContent)
})
```

Pass the raw body and the headers: `r.Header`, or `emailvalidation.HeadersFromMap(m)` for a `map[string]string`
(from a framework or a queue message). Names match in any case; several values of a name are joined with a space,
and signatures joined with `, ` still verify. Deliveries follow the Standard Webhooks scheme (`webhook-id`,
`webhook-timestamp`, `webhook-signature`, HMAC-SHA256 with your `whsec_…` secret, compared in constant time). The
timestamp must be within `DefaultWebhookTolerance` (5 minutes) of now (`WebhookOptions.Tolerance`;
`WebhookOptions.Now` for tests). Failures return a `*emailvalidation.WebhookVerificationError` whose message is one
of: `Missing webhook headers.`, `Invalid webhook timestamp.`, `Webhook timestamp is outside the allowed tolerance.`,
`Missing webhook secret.`, `The webhook secret is not valid (expected whsec_ followed by base64).`,
`No webhook signature matched.`, `The webhook body is not JSON.` (the body must be a JSON object).

Deduplicate on the `webhook-id` header (retries reuse it) and answer 2xx quickly. `SignWebhook` builds a signature
for testing your endpoint locally.

## Constants

`Statuses`, `Reasons`, `ReasonsByStatus`, `ReasonInfo` (status and action of every reason), `Actions`,
`SMTPProviders`, `BulkJobStatuses`, `TerminalBulkJobStatuses`, `BulkJobErrorCodes`, `ErrorCodes`,
`ClientErrorCodes`, `WebhookEvents`, `DefaultBaseURL`, `EUBaseURL`, `SandboxDomain`, `DefaultTimeout`,
`DefaultMaxRetries`, `DefaultMaxRetryWait`, `DefaultWebhookTolerance` (300 s), `Version`, the typed constants
(`StatusValid`, `ReasonAccepted`, `ActionSend`, `SMTPProviderGoogle`, `BulkJobStatusCompleted`, `CodeRateLimited`,
`ResultsFilterAll`, `FilterAll`, …), `IsStatus`, `IsReason`, `FilterStatuses` and `SandboxAddress`. The lists are shared: do not modify
them.

## Bring your own HTTP client

`WithHTTPClient(*http.Client)` uses your client (proxies, custom TLS); it is copied so that redirects are never
followed, and never closed. Two settings of that client work against the SDK:

- Leave `http.Client.Timeout` at zero. It caps the whole exchange, so it cuts a slow but progressing upload or
  download that the SDK's idle timeout lets finish; use `WithTimeout` or `WithRequestTimeout` instead.
- Its `Transport` (the `RoundTripper`) must not retry by itself. The SDK sets `GetBody` on every request with a body,
  so a retrying `RoundTripper` can send a `Validate`, bulk upload, start or cancel again after the connection failed,
  although it may already have been charged or applied. Let the SDK do the retries.

To replace the HTTP stack entirely, implement `Transport` (`Do(ctx context.Context, req *Request) (*Response,
error)`) and pass it with `WithTransport`:

```go
// loggingTransport wraps the default transport. Request's string form hides the key and the body.
type loggingTransport struct {
	next emailvalidation.Transport
}

func (t loggingTransport) Do(ctx context.Context, req *emailvalidation.Request) (*emailvalidation.Response, error) {
	log.Println("sending", req) // Request{POST https://api.email-validation.org/v1/validate, headers and body hidden}
	return t.next.Do(ctx, req)
}

func newClient() (*emailvalidation.Client, error) {
	return emailvalidation.New(os.Getenv("EV_API_KEY"),
		emailvalidation.WithTransport(loggingTransport{next: emailvalidation.NewHTTPTransport(nil)}))
}
```

`Request` has `Method`, `URL`, `Header`, `Body` and `Timeout`. Return a `*Response` for any HTTP answer (do not
follow redirects), or a `*TransportError` (`Timeout: true` for a timeout) when no answer arrived. Send `Body` with
its length (the API refuses chunked uploads) and honour `ctx`. A transport other than the built-in one cannot report
upload progress, so an upload through it gets `timeout + size ÷ 128 KiB/s` before the idle rules apply.

## Privacy

The API key is sent only in the `Authorization` header, never in a URL. It never appears in the string or debug
form of the client, a `Request` or an error (every `fmt` verb is covered), and errors never contain the request body
or the checked address. The SDK writes nothing to stdout or stderr and sends no telemetry.

## Licence

MIT, see `LICENSE`.
