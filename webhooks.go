package emailvalidation

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Webhook signatures follow the Standard Webhooks scheme: HMAC-SHA256 over "<webhook-id>.<webhook-timestamp>.<raw
// body>" with the endpoint secret (the base64 part after "whsec_"); header "webhook-signature: v1,<base64>
// [v1,<base64> …]".

// Messages of WebhookVerificationError.
const (
	msgWebhookMissingHeaders = "Missing webhook headers."
	msgWebhookBadTimestamp   = "Invalid webhook timestamp."
	msgWebhookTolerance      = "Webhook timestamp is outside the allowed tolerance."
	msgWebhookMissingSecret  = "Missing webhook secret."
	msgWebhookBadSecret      = "The webhook secret is not valid (expected whsec_ followed by base64)."
	msgWebhookNoMatch        = "No webhook signature matched."
	msgWebhookNotJSON        = "The webhook body is not JSON."
)

// WebhookOptions are the optional arguments of VerifyWebhook.
type WebhookOptions struct {
	// Tolerance is the largest accepted difference between the webhook timestamp and now (0 = 300 s).
	Tolerance time.Duration
	// Now replaces the current time (tests).
	Now time.Time
}

func webhookError(msg string) error { return &WebhookVerificationError{Message: msg} }

// header looks a name up case-insensitively; several values (or keys differing only in case) are joined with a
// space.
func header(h http.Header, name string) string {
	var keys []string
	for k := range h {
		if strings.EqualFold(k, name) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var values []string
	for _, k := range keys {
		values = append(values, h[k]...)
	}
	return strings.Join(values, " ")
}

// HeadersFromMap turns a map of header names to single values (from a framework or a queue message) into the
// http.Header that VerifyWebhook takes. The names are kept as given: VerifyWebhook looks them up in any case.
func HeadersFromMap(m map[string]string) http.Header {
	h := make(http.Header, len(m))
	for k, v := range m {
		h[k] = append(h[k], v)
	}
	return h
}

func validTimestamp(ts string) bool {
	return len(ts) >= 1 && len(ts) <= 12 && isDigits(ts)
}

// decodeStdBase64 is strict standard base64 (RFC 4648 §4, padded): line breaks are refused too.
func decodeStdBase64(s string) ([]byte, error) {
	if strings.ContainsAny(s, "\r\n") {
		return nil, base64.CorruptInputError(strings.IndexAny(s, "\r\n"))
	}
	return base64.StdEncoding.DecodeString(s)
}

func webhookKey(secret string) ([]byte, error) {
	if secret == "" {
		return nil, webhookError(msgWebhookMissingSecret)
	}
	key, err := decodeStdBase64(strings.TrimPrefix(secret, "whsec_"))
	if err != nil || len(key) == 0 {
		return nil, webhookError(msgWebhookBadSecret)
	}
	return key, nil
}

func webhookMAC(key []byte, id, timestamp string, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(id + "." + timestamp + "."))
	m.Write(payload)
	return m.Sum(nil)
}

// SignWebhook builds the webhook-signature value ("v1,<base64>") for a payload. Use it to test your endpoint
// locally. It returns a *WebhookVerificationError for an empty or invalid secret.
func SignWebhook(msgID string, timestamp int64, payload []byte, secret string) (string, error) {
	key, err := webhookKey(secret)
	if err != nil {
		return "", err
	}
	mac := webhookMAC(key, msgID, strconv.FormatInt(timestamp, 10), payload)
	return "v1," + base64.StdEncoding.EncodeToString(mac), nil
}

// VerifyWebhook checks a delivery and returns the parsed event. Pass the raw request body (before any JSON
// parsing) and the request headers: r.Header, or HeadersFromMap for a map of single values. Names match in any
// case; several values of a name are joined with a space, and signatures joined with ", " still verify. It returns a *WebhookVerificationError when headers are
// missing, the timestamp is malformed or outside the tolerance, the secret is invalid, no signature matches or the
// body is not JSON. opts may be nil.
//
// Deduplicate deliveries on the webhook-id header (retries reuse it) and answer 2xx quickly.
func VerifyWebhook(payload []byte, headers http.Header, secret string, opts *WebhookOptions) (*WebhookEvent, error) {
	id := header(headers, "webhook-id")
	timestamp := header(headers, "webhook-timestamp")
	signatures := header(headers, "webhook-signature")
	if id == "" || timestamp == "" || signatures == "" {
		return nil, webhookError(msgWebhookMissingHeaders)
	}
	if !validTimestamp(timestamp) {
		return nil, webhookError(msgWebhookBadTimestamp)
	}
	tolerance := DefaultWebhookTolerance
	now := time.Now()
	if opts != nil {
		if opts.Tolerance != 0 {
			tolerance = opts.Tolerance
		}
		if !opts.Now.IsZero() {
			now = opts.Now
		}
	}
	ts, _ := strconv.ParseInt(timestamp, 10, 64) // at most 12 digits: never overflows
	if math.Abs(float64(now.Unix()-ts)) > tolerance.Seconds() {
		return nil, webhookError(msgWebhookTolerance)
	}

	key, err := webhookKey(secret)
	if err != nil {
		return nil, err
	}
	expected := webhookMAC(key, id, timestamp, payload)
	for _, entry := range strings.Fields(signatures) {
		// A stack that joins repeated header lines with ", " leaves a trailing comma on all but the last entry.
		version, sig, found := strings.Cut(strings.TrimSuffix(entry, ","), ",")
		if !found || version != "v1" || sig == "" {
			continue
		}
		given, err := decodeStdBase64(sig)
		if err != nil {
			continue
		}
		if hmac.Equal(given, expected) { // constant time
			if !isJSONObject(payload) || !json.Valid(payload) {
				return nil, webhookError(msgWebhookNotJSON)
			}
			// Any object is an event (forward compatible): a field of an unexpected type is left empty.
			var event WebhookEvent
			_ = json.Unmarshal(payload, &event)
			return &event, nil
		}
	}
	return nil, webhookError(msgWebhookNoMatch)
}

// isJSONObject reports whether the body starts as a JSON object (null, arrays and scalars are not events).
func isJSONObject(payload []byte) bool {
	trimmed := bytes.TrimLeft(payload, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}
