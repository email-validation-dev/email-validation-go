// Package emailvalidation is the official Go client for the email-validation.org REST API: validate single
// addresses, read credits, usage and service status, run bulk jobs and verify webhook signatures.
//
// It uses only the standard library (net/http, encoding/json, crypto/hmac) and needs Go 1.22 or later.
//
//	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"))
//	if err != nil {
//		log.Fatal(err)
//	}
//	result, err := client.Validate(ctx, "accepted@sandbox.email-validation.org", nil) // free test address
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(result.Status, result.Reason, result.Action)
//
// # Errors
//
// Every API call returns an *Error. Branch on its Kind (or errors.Is with a sentinel such as ErrRateLimited) or on
// its Code, never on its Message. Wrong arguments are refused before any request with an error wrapping
// ErrInvalidArgument (except a malformed bulk job id, which is an *Error of KindInvalidRequest with status 0, as in
// every SDK of this API). Webhook checks return a *WebhookVerificationError.
//
// # Retries
//
// 429 and 503 answers are never charged, so they are retried for every method, honouring Retry-After, up to
// WithMaxRetries times (default 2) as long as the wait is at most WithMaxRetryWait (default 10 s). key_daily_cap
// is not retried (it resets the next day). A request that got no answer (network failure or timeout) is retried
// only for GET: a Validate that reached the service may already have been charged. Nothing else is retried.
//
// # Cancellation
//
// Every method takes a context.Context. Cancelling it (or its deadline passing) stops the call and any retry or poll
// wait at once with a ConnectionError of Code CodeAborted; errors.Is(err, context.Canceled) (or
// context.DeadlineExceeded) also holds. A cancelled call is never retried. The per-attempt timeout (WithTimeout,
// default 30 s) is separate and gives Code CodeTimeout.
package emailvalidation
