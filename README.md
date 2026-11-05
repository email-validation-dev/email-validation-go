# email-validation.org Go SDK

Official Go client for the [email-validation.org](https://email-validation.org) API: validate single addresses, read
credits, usage and service status, run bulk jobs and verify webhook signatures. Standard library only (`net/http`,
`encoding/json`, `crypto/hmac`); no dependencies. Needs Go 1.22 or later; tested up to Go 1.27.

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

Create the client once and share it: it is safe for concurrent use. Keep the API key on the server.
`<reason>@sandbox.email-validation.org` addresses are free and answer that reason
(`emailvalidation.SandboxAddress(emailvalidation.ReasonCatchAll)`).

## Client options

```go
client, err := emailvalidation.New(apiKey,
	emailvalidation.WithBaseURL("https://api.email-validation.org"), // default
	emailvalidation.WithTimeout(30*time.Second),                     // per attempt (default 30 s)
	emailvalidation.WithMaxRetries(2),                               // retries after the first attempt (default 2)
	emailvalidation.WithMaxRetryWait(10*time.Second),                // longest accepted wait before a retry (default 10 s)
	emailvalidation.WithHTTPClient(myHTTPClient),                    // optional: your *http.Client
)
```

EU data residency (validated and stored in the EU): pass `emailvalidation.WithBaseURL(emailvalidation.EUBaseURL)` to call `https://api-eu.email-validation.org`
instead of the default `https://api.email-validation.org`. See https://email-validation.org/docs/eu-data-residency/.

`New` returns an error wrapping `emailvalidation.ErrInvalidArgument` for an empty key, a key containing whitespace,
a base URL that does not start with `http://` or `https://`, or a timeout that is not positive. `client.Close()`
releases idle connections of the HTTP client the SDK created (never one you supplied).

## Methods

Every method takes a `context.Context` first. Optional arguments are an options struct pointer; `nil` sends none.

| Method | Request | Returns |
|---|---|---|
| `client.Validate(ctx, email, *ValidateOptions)` | `POST /v1/validate` | `*ValidationResult` |
| `client.Credits(ctx)` | `GET /v1/credits` | `*Credits` |
| `client.Usage(ctx, *UsageParams)` | `GET /v1/usage` | `*UsageResponse` |
| `client.Status(ctx)` | `GET /v1/status` | `*ServiceStatus` |
| `client.Bulk.Create(ctx, *BulkCreateParams)` | `POST /v1/bulk` | `*BulkJob` |
| `client.Bulk.Get(ctx, jobID)` | `GET /v1/bulk/{id}` | `*BulkJob` |
| `client.Bulk.Start(ctx, jobID)` | `POST /v1/bulk/{id}/start` | `*BulkJob` |
| `client.Bulk.Cancel(ctx, jobID)` | `DELETE /v1/bulk/{id}` | `*BulkJob` |
| `client.Bulk.Results(ctx, jobID, *ResultsOptions)` | `GET /v1/bulk/{id}/results` | `[]byte` (CSV) |
| `client.Bulk.ResultsStream(ctx, jobID, *ResultsOptions)` | same | `io.ReadCloser` (CSV) |
| `client.Bulk.Wait(ctx, jobID, *WaitOptions)` | polls `Get` | `*BulkJob` |
| `emailvalidation.VerifyWebhook(body, headers, secret, *WebhookOptions)` | none | `*WebhookEvent` |
| `emailvalidation.SignWebhook(msgID, timestamp, body, secret)` | none | `"v1,<base64>"` |

### Validate an address

```go
result, err := client.Validate(ctx, "accepted@sandbox.email-validation.org", &emailvalidation.ValidateOptions{
	IPAddress:      "203.0.113.7",             // the IP the person used when they gave you the address
	TimeoutSeconds: 10,                        // time the service may spend: 5–15 s (default 15)
	Fresh:          emailvalidation.Bool(true), // check again even if checked in the last 12 months
})
```

A check costs 1 credit unless the result is `unknown` or answered locally (`result.CreditsUsed` is `0` or `1`).
`result.Status` is `valid`, `invalid`, `catch_all`, `unknown` or `disposable`; `result.Reason` explains it and
`result.Action` says what to do (`send`, `send_with_caution`, `remove`, `retry_later`). Spam traps and frequent
complainers are `valid` with the action `remove` (reason `spamtrap` / `complainer`). Nullable fields are pointers
(`nil` = JSON `null`), and times are the API's exact ISO 8601 UTC strings. A status, reason, action or provider that
this SDK version does not know yet is kept as its raw string, so decoding never fails on a new value.

### Credits, usage and status

```go
credits, err := client.Credits(ctx)             // Balance, Debt, Pending, Lots (soonest-expiring first)
usage, err := client.Usage(ctx, &emailvalidation.UsageParams{
	From: "2026-10-01", To: "2026-10-20",      // YYYY-MM-DD, UTC
	GroupBy: emailvalidation.GroupByKey,       // GroupByDay (default) fills usage.Days, GroupByKey fills usage.Keys
})
status, err := client.Status(ctx)               // Status and Components (Website, API, Bulk, SignIn, Payments, Support)
```

### Bulk jobs

```go
job, err := client.Bulk.Create(ctx, &emailvalidation.BulkCreateParams{
	Emails: []string{"accepted@sandbox.email-validation.org", "no_mailbox@sandbox.email-validation.org"},
})
// or from a file (CSV, or one address per line with ContentType: emailvalidation.ContentTypeText):
f, _ := os.Open("contacts.csv")
job, err = client.Bulk.Create(ctx, &emailvalidation.BulkCreateParams{
	File: f, ColumnName: "email", HasHeader: emailvalidation.Bool(true),
})

job, err = client.Bulk.Wait(ctx, job.ID, &emailvalidation.WaitOptions{
	Interval: 5 * time.Second, // default 5 s, at least 1 s
	Timeout:  30 * time.Minute, // default: no limit
	OnProgress: func(j *emailvalidation.BulkJob) { log.Println(j.Status, j.Processed) },
})
if job.Status == emailvalidation.BulkJobStatusCompleted {
	csv, err := client.Bulk.Results(ctx, job.ID, &emailvalidation.ResultsOptions{
		Filter: []emailvalidation.Status{emailvalidation.StatusValid, emailvalidation.StatusCatchAll},
	})
	if err == nil {
		os.Stdout.Write(csv)
	}
}
```

- `Create` takes exactly one of `Emails` (sent as JSON, up to 100,000 addresses) or `File` (an `io.Reader`, up to
  100 MB, read into memory first because the API needs a known length and retries re-send the same bytes). Options:
  `ColumnIndex` (1-based) or `ColumnName`, `HasHeader`, `Start` (set `emailvalidation.Bool(false)` to start later
  with `Bulk.Start`) and `WebhookEndpointID`. Bulk calls need a key with the `bulk` scope.
- `Wait` returns the job once it is `completed`, `failed`, `cancelled` or `expired`, or once it is `ready` with an
  `ErrorCode` (its start was refused, e.g. `insufficient_credits`: buy credits, then call `Start`). Check
  `job.Status` before downloading results. A job created with `Start: false` stays in `ready` until you call `Start`,
  so call `Start` before `Wait`. When `Timeout` passes, `Wait` returns a `ConnectionError` with code `timeout`.
- `Results` returns the whole CSV; `ResultsStream` returns an `io.ReadCloser` for large files (close it). The client
  timeout covers the whole download, so raise `WithTimeout` for big files. `Filter` is `emailvalidation.FilterAll`,
  one status or several; `Columns` is `ColumnsOriginal` (default) or `ColumnsEmailOnly`; `EscapeFormulas` defaults
  to true on the server. `Range` (e.g. `"bytes=0-1048575"`) fetches one part of the file to resume a download; use it
  with `FilterAll` (or one status with `EscapeFormulas: emailvalidation.Bool(false)`) and `ColumnsOriginal`. Other
  shapes cannot be served in parts and return an `InvalidRequestError` instead of the whole file.
- A malformed job id is refused before any request (`InvalidRequestError`, status 0, code `invalid_request`).

## Errors

Every API call returns an `*emailvalidation.Error`. Branch on its kind or code, never on the message:

```go
_, err := client.Validate(ctx, address, nil)
var apiErr *emailvalidation.Error
switch {
case errors.Is(err, emailvalidation.ErrInsufficientCredits):
	// buy credits (nothing was charged)
case errors.Is(err, emailvalidation.ErrRateLimited):
	if errors.As(err, &apiErr) && apiErr.RetryAfter != nil {
		time.Sleep(time.Duration(*apiErr.RetryAfter) * time.Second)
	}
case errors.As(err, &apiErr):
	log.Println(apiErr.Kind, apiErr.Status, apiErr.Code, apiErr.RequestID)
}
```

`*Error` fields: `Kind`, `Status` (HTTP status, `0` when no answer arrived), `Code`, `Message` (for people; may
change), `RequestID` (quote it to support; `""` when absent), `RetryAfter` (`*int` seconds) and `Err` (the cause,
for `errors.Unwrap`). Its string form shows the kind, status, code, request id and message, never the key, the
request body or the checked address.

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
| `KindConnection` (`ErrConnection`) | no answer, cancelled, `Bulk.Wait` deadline | `connection_error`, `timeout`, `aborted` |
| `KindEmailValidation` (`ErrUnexpectedResponse`) | a 2xx answer that is not JSON | `unexpected_response` |

An answer without our error envelope (or with an unknown code) gets code `unexpected_response` and the message
`Unexpected HTTP <status> from the API.` Wrong arguments (both or neither of `Emails` and `File`, an unknown
`ContentType` or results filter) return an error wrapping `emailvalidation.ErrInvalidArgument` before any request.

## Retries, timeouts and cancellation

- `429` and `503` answers are never charged, so they are retried for every method, `POST` included, up to
  `WithMaxRetries` times (default 2). The wait is `Retry-After` (seconds or an HTTP date) when the answer has one,
  otherwise exponential backoff with jitter (0.25–0.5 s, then 0.5–1 s, 1–2 s, … capped at 4–8 s). A wait longer
  than `WithMaxRetryWait` (default 10 s) ends the call with the error instead. `key_daily_cap` is never retried (it
  resets the next day).
- A request that got no answer (network failure or timeout) is retried only for `GET`: a `Validate`, bulk upload,
  start or cancel that reached the service may already have been charged or applied.
- Nothing else is retried (400–428, 500, 502, 504, …). Retries re-send the same bytes.
- `WithTimeout` (default 30 s) bounds each attempt, connect and full response, not the whole call. The
  `TimeoutSeconds` of `Validate` (5–15 s) is the time the service may spend; keep the client timeout a few seconds
  above it.
- Cancelling the context (or its deadline passing) stops the call at once, during a request or a retry or poll wait,
  with a `ConnectionError` of code `aborted`; `errors.Is(err, context.Canceled)` (or `context.DeadlineExceeded`)
  also holds. A cancelled call is never retried.

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
	switch event.Type {
	case emailvalidation.WebhookBulkCompleted, emailvalidation.WebhookBulkFailed:
		data, _ := event.BulkData() // JobID, Status, Processed, CreditsCharged, Counts, ErrorCode
		log.Println(data.JobID, data.Status)
	case emailvalidation.WebhookCreditsLow: // event.CreditsLowData(): Balance, Threshold
	case emailvalidation.WebhookCreditsExpiring: // event.CreditsExpiringData(): Credits, ExpiresAt
	case emailvalidation.WebhookTest: // "Send test" in the dashboard
	}
	w.WriteHeader(http.StatusNoContent)
})
```

Deliveries follow the Standard Webhooks scheme (`webhook-id`, `webhook-timestamp`, `webhook-signature` headers,
HMAC-SHA256 with your `whsec_…` secret, compared in constant time). Header names are matched in any case. The
timestamp must be within 5 minutes of now (`WebhookOptions.Tolerance`, `WebhookOptions.Now` for tests). Failures
return a `*emailvalidation.WebhookVerificationError` whose message is one of: `Missing webhook headers.`,
`Invalid webhook timestamp.`, `Webhook timestamp is outside the allowed tolerance.`, `Missing webhook secret.`,
`The webhook secret is not valid (expected whsec_ followed by base64).`, `No webhook signature matched.`,
`The webhook body is not JSON.`

Deduplicate on the `webhook-id` header (retries reuse it) and answer 2xx quickly. `SignWebhook` builds a
signature for testing your endpoint locally.

## Constants

`Statuses`, `Reasons`, `ReasonsByStatus`, `ReasonInfo` (status and action of every reason), `Actions`,
`SMTPProviders`, `BulkJobStatuses`, `TerminalBulkJobStatuses`, `BulkJobErrorCodes`, `ErrorCodes`,
`ClientErrorCodes`, `WebhookEvents`, `DefaultBaseURL`, `EUBaseURL`, `SandboxDomain`, `DefaultWebhookTolerance` (300 s),
`Version`, the typed constants (`StatusValid`, `ReasonAccepted`, `ActionSend`, `SMTPProviderGoogle`,
`BulkJobStatusCompleted`, `CodeRateLimited`, …), `IsStatus`, `IsReason` and `SandboxAddress`. The lists are shared:
do not modify them.

## Bring your own HTTP client

`WithHTTPClient(*http.Client)` uses your client (proxies, custom TLS); it is copied so that redirects are never
followed, and never closed. To replace the HTTP stack entirely, implement `Transport`:

```go
type Transport interface {
	Do(ctx context.Context, req *emailvalidation.Request) (*emailvalidation.Response, error)
}
```

`Request` has `Method`, `URL`, `Header`, `Body` and `Timeout`; its string forms hide the headers and the body (they
carry the key and the checked address). Return a `*Response` for any HTTP answer (do not follow redirects), or a
`*TransportError` (`Timeout: true` for a timeout) when no answer arrived. `NewHTTPTransport` gives the default
transport so you can wrap it:

```go
client, err := emailvalidation.New(apiKey,
	emailvalidation.WithTransport(loggingTransport{next: emailvalidation.NewHTTPTransport(nil)}))
```

## Licence

MIT (see `LICENSE`).
