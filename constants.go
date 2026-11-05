package emailvalidation

import "time"

// Version is the SDK version. It is sent in the User-Agent header.
const Version = "1.0.0"

// DefaultBaseURL is the API's base URL.
const DefaultBaseURL = "https://api.email-validation.org"

// EUBaseURL is the EU endpoint (EU data residency: validated and stored in the EU). Pass it to WithBaseURL.
const EUBaseURL = "https://api-eu.email-validation.org"

// SandboxDomain is the domain of the free test addresses: <reason>@sandbox.email-validation.org answers that
// reason and costs nothing.
const SandboxDomain = "sandbox.email-validation.org"

// Client defaults.
const (
	DefaultTimeout      = 30 * time.Second
	DefaultMaxRetries   = 2
	DefaultMaxRetryWait = 10 * time.Second
)

// DefaultWebhookTolerance is the largest accepted difference between a webhook's timestamp and the current time.
const DefaultWebhookTolerance = 300 * time.Second

// Status is the verdict of a check. Unknown future values are kept as the raw string.
type Status string

// Statuses.
const (
	StatusValid      Status = "valid"
	StatusInvalid    Status = "invalid"
	StatusCatchAll   Status = "catch_all"
	StatusUnknown    Status = "unknown"
	StatusDisposable Status = "disposable"
)

// FilterAll is the results filter that keeps every row (the server default). It is only meaningful in
// ResultsOptions.Filter.
const FilterAll Status = "all"

// Reason explains a status. Each reason has a fixed status and action (see ReasonInfo). Unknown future values are
// kept as the raw string.
type Reason string

// Reasons, grouped by status.
const (
	// valid
	ReasonAccepted        Reason = "accepted"
	ReasonAlias           Reason = "alias"
	ReasonNormalized      Reason = "normalized"
	ReasonLowEngagement   Reason = "low_engagement"
	ReasonHighEngagement  Reason = "high_engagement"
	ReasonTrustedCatchAll Reason = "trusted_catch_all"
	ReasonAIAgent         Reason = "ai_agent"
	ReasonRoleAccount     Reason = "role_account"
	ReasonAbuseLinked     Reason = "abuse_linked"
	ReasonSuppressed      Reason = "suppressed"
	ReasonTrapPattern     Reason = "trap_pattern"
	ReasonSpamtrap        Reason = "spamtrap"
	ReasonComplainer      Reason = "complainer"
	// invalid
	ReasonNoMailbox     Reason = "no_mailbox"
	ReasonNoInboundMail Reason = "no_inbound_mail"
	ReasonInvalidSyntax Reason = "invalid_syntax"
	ReasonLikelyTypo    Reason = "likely_typo"
	ReasonNoDNS         Reason = "no_dns"
	ReasonMailboxFull   Reason = "mailbox_full"
	ReasonUnroutable    Reason = "unroutable"
	// catch_all
	ReasonCatchAll         Reason = "catch_all"
	ReasonRoleCatchAll     Reason = "role_catch_all"
	ReasonForwardingDomain Reason = "forwarding_domain"
	// unknown
	ReasonServerUnavailable Reason = "server_unavailable"
	ReasonTemporaryError    Reason = "temporary_error"
	ReasonGreylisted        Reason = "greylisted"
	ReasonAntiSpamBlock     Reason = "anti_spam_block"
	ReasonTimeout           Reason = "timeout"
	ReasonInternalError     Reason = "internal_error"
	// disposable
	ReasonDisposable Reason = "disposable"
)

// Action is what to do with an address. Unknown future values are kept as the raw string.
type Action string

// Actions.
const (
	ActionSend            Action = "send"
	ActionSendWithCaution Action = "send_with_caution"
	ActionRemove          Action = "remove"
	ActionRetryLater      Action = "retry_later"
)

// SMTPProvider is the mailbox provider behind a domain. Unknown future values are kept as the raw string.
type SMTPProvider string

// SMTP providers.
const (
	SMTPProviderGoogle     SMTPProvider = "google"
	SMTPProviderMicrosoft  SMTPProvider = "microsoft"
	SMTPProviderYahoo      SMTPProvider = "yahoo"
	SMTPProviderApple      SMTPProvider = "apple"
	SMTPProviderZoho       SMTPProvider = "zoho"
	SMTPProviderProton     SMTPProvider = "proton"
	SMTPProviderFastmail   SMTPProvider = "fastmail"
	SMTPProviderGMX        SMTPProvider = "gmx"
	SMTPProviderYandex     SMTPProvider = "yandex"
	SMTPProviderMailru     SMTPProvider = "mailru"
	SMTPProviderAmazon     SMTPProvider = "amazon"
	SMTPProviderMimecast   SMTPProvider = "mimecast"
	SMTPProviderProofpoint SMTPProvider = "proofpoint"
	SMTPProviderBarracuda  SMTPProvider = "barracuda"
	SMTPProviderOther      SMTPProvider = "other"
)

// BulkJobStatus is the state of a bulk job. Unknown future values are kept as the raw string.
type BulkJobStatus string

// Bulk job statuses.
const (
	BulkJobStatusAnalyzing  BulkJobStatus = "analyzing"
	BulkJobStatusReady      BulkJobStatus = "ready"
	BulkJobStatusQueued     BulkJobStatus = "queued"
	BulkJobStatusRunning    BulkJobStatus = "running"
	BulkJobStatusCancelling BulkJobStatus = "cancelling"
	BulkJobStatusFinalizing BulkJobStatus = "finalizing"
	BulkJobStatusCompleted  BulkJobStatus = "completed"
	BulkJobStatusFailed     BulkJobStatus = "failed"
	BulkJobStatusCancelled  BulkJobStatus = "cancelled"
	BulkJobStatusExpired    BulkJobStatus = "expired"
)

// IsTerminal reports whether a job in this status never changes again (completed, failed, cancelled, expired).
func (s BulkJobStatus) IsTerminal() bool {
	for _, t := range TerminalBulkJobStatuses {
		if s == t {
			return true
		}
	}
	return false
}

// BulkJobErrorCode says why a job failed or waits in ready. Unknown future values are kept as the raw string.
type BulkJobErrorCode string

// Bulk job error codes. The first group comes with status failed, the second with status ready (the start was
// refused; fix the cause, then call Bulk.Start).
const (
	BulkJobErrorInvalidCSV          BulkJobErrorCode = "invalid_csv"
	BulkJobErrorTooManyRows         BulkJobErrorCode = "too_many_rows"
	BulkJobErrorEmptyFile           BulkJobErrorCode = "empty_file"
	BulkJobErrorEmailColumnNotFound BulkJobErrorCode = "email_column_not_found"
	BulkJobErrorUploadMissing       BulkJobErrorCode = "upload_missing"
	BulkJobErrorPipelineMismatch    BulkJobErrorCode = "pipeline_mismatch"
	BulkJobErrorMissingResults      BulkJobErrorCode = "missing_results"
	BulkJobErrorInternalError       BulkJobErrorCode = "internal_error"
	BulkJobErrorInsufficientCredits BulkJobErrorCode = "insufficient_credits"
	BulkJobErrorUpstreamCapacity    BulkJobErrorCode = "upstream_capacity"
	BulkJobErrorAccountSuspended    BulkJobErrorCode = "account_suspended"
	BulkJobErrorBillingHold         BulkJobErrorCode = "billing_hold"
	BulkJobErrorInvalidKey          BulkJobErrorCode = "invalid_key"
	BulkJobErrorForbiddenScope      BulkJobErrorCode = "forbidden_scope"
	BulkJobErrorKeyDailyCap         BulkJobErrorCode = "key_daily_cap"
	BulkJobErrorUploadExpired       BulkJobErrorCode = "upload_expired"
)

// ComponentState is the state of the service or one of its components.
type ComponentState string

// Component states.
const (
	ComponentOperational ComponentState = "operational"
	ComponentDegraded    ComponentState = "degraded"
	ComponentOutage      ComponentState = "outage"
)

// UsageGroupBy selects how Usage groups its items.
type UsageGroupBy string

// Usage groupings.
const (
	GroupByDay UsageGroupBy = "day"
	GroupByKey UsageGroupBy = "key"
)

// ResultsColumns selects the columns of a results download.
type ResultsColumns string

// Results column sets.
const (
	ColumnsOriginal  ResultsColumns = "original"
	ColumnsEmailOnly ResultsColumns = "email_only"
)

// File content types accepted by Bulk.Create.
const (
	ContentTypeCSV  = "text/csv"
	ContentTypeText = "text/plain"
)

// WebhookEventType is the type of a webhook delivery.
type WebhookEventType string

// Webhook event types.
const (
	WebhookBulkCompleted   WebhookEventType = "bulk.completed"
	WebhookBulkFailed      WebhookEventType = "bulk.failed"
	WebhookCreditsLow      WebhookEventType = "credits.low"
	WebhookCreditsExpiring WebhookEventType = "credits.expiring"
	// WebhookTest is sent only by "Send test" in the dashboard; answer it with 2xx.
	WebhookTest WebhookEventType = "webhook.test"
)

// ErrorCode is an API error code or one of the client's own codes.
type ErrorCode string

// API error codes (the code of the error envelope).
const (
	CodeInvalidRequest      ErrorCode = "invalid_request"
	CodeUnauthorized        ErrorCode = "unauthorized"
	CodeInsufficientCredits ErrorCode = "insufficient_credits"
	CodeForbidden           ErrorCode = "forbidden"
	CodeForbiddenOrigin     ErrorCode = "forbidden_origin"
	CodeNotFound            ErrorCode = "not_found"
	CodeConflict            ErrorCode = "conflict"
	CodeLengthRequired      ErrorCode = "length_required"
	CodePayloadTooLarge     ErrorCode = "payload_too_large"
	CodeRateLimited         ErrorCode = "rate_limited"
	CodeAbuseThrottled      ErrorCode = "abuse_throttled"
	CodeKeyDailyCap         ErrorCode = "key_daily_cap"
	CodeInternalError       ErrorCode = "internal_error"
	CodeServiceUnavailable  ErrorCode = "service_unavailable"
	CodeUpstreamCapacity    ErrorCode = "upstream_capacity"
)

// Client error codes, for failures that got no usable API answer.
const (
	CodeConnectionError    ErrorCode = "connection_error"
	CodeTimeout            ErrorCode = "timeout"
	CodeUnexpectedResponse ErrorCode = "unexpected_response"
	CodeAborted            ErrorCode = "aborted"
)

// ReasonDetail is the fixed status and action of a reason.
type ReasonDetail struct {
	Status Status
	Action Action
}

// The vocabulary lists below are shared, read-only values: do not modify them.
var (
	// Statuses lists every status.
	Statuses = []Status{
		StatusValid, StatusInvalid, StatusCatchAll, StatusUnknown, StatusDisposable,
	}

	// ReasonsByStatus lists the reasons of each status.
	ReasonsByStatus = map[Status][]Reason{
		StatusValid: {
			ReasonAccepted, ReasonAlias, ReasonNormalized, ReasonLowEngagement, ReasonHighEngagement,
			ReasonTrustedCatchAll, ReasonAIAgent, ReasonRoleAccount, ReasonAbuseLinked, ReasonSuppressed,
			ReasonTrapPattern, ReasonSpamtrap, ReasonComplainer,
		},
		StatusInvalid: {
			ReasonNoMailbox, ReasonNoInboundMail, ReasonInvalidSyntax, ReasonLikelyTypo, ReasonNoDNS,
			ReasonMailboxFull, ReasonUnroutable,
		},
		StatusCatchAll: {ReasonCatchAll, ReasonRoleCatchAll, ReasonForwardingDomain},
		StatusUnknown: {
			ReasonServerUnavailable, ReasonTemporaryError, ReasonGreylisted, ReasonAntiSpamBlock, ReasonTimeout,
			ReasonInternalError,
		},
		StatusDisposable: {ReasonDisposable},
	}

	// Reasons lists every reason, in catalogue order (by status, unknown last).
	Reasons = func() []Reason {
		var out []Reason
		for _, s := range []Status{
			StatusValid, StatusInvalid, StatusCatchAll, StatusDisposable, StatusUnknown,
		} {
			out = append(out, ReasonsByStatus[s]...)
		}
		return out
	}()

	// ReasonInfo gives the fixed status and action of every reason.
	ReasonInfo = map[Reason]ReasonDetail{
		ReasonAccepted:          {StatusValid, ActionSend},
		ReasonAlias:             {StatusValid, ActionSend},
		ReasonNormalized:        {StatusValid, ActionSend},
		ReasonLowEngagement:     {StatusValid, ActionSendWithCaution},
		ReasonHighEngagement:    {StatusValid, ActionSend},
		ReasonTrustedCatchAll:   {StatusValid, ActionSend},
		ReasonAIAgent:           {StatusValid, ActionSendWithCaution},
		ReasonRoleAccount:       {StatusValid, ActionSendWithCaution},
		ReasonAbuseLinked:       {StatusValid, ActionRemove},
		ReasonSuppressed:        {StatusValid, ActionRemove},
		ReasonTrapPattern:       {StatusValid, ActionRemove},
		ReasonSpamtrap:          {StatusValid, ActionRemove},
		ReasonComplainer:        {StatusValid, ActionRemove},
		ReasonNoMailbox:         {StatusInvalid, ActionRemove},
		ReasonNoInboundMail:     {StatusInvalid, ActionRemove},
		ReasonInvalidSyntax:     {StatusInvalid, ActionRemove},
		ReasonLikelyTypo:        {StatusInvalid, ActionRemove},
		ReasonNoDNS:             {StatusInvalid, ActionRemove},
		ReasonMailboxFull:       {StatusInvalid, ActionRemove},
		ReasonUnroutable:        {StatusInvalid, ActionRemove},
		ReasonCatchAll:          {StatusCatchAll, ActionSendWithCaution},
		ReasonRoleCatchAll:      {StatusCatchAll, ActionSendWithCaution},
		ReasonForwardingDomain:  {StatusCatchAll, ActionSendWithCaution},
		ReasonDisposable:        {StatusDisposable, ActionRemove},
		ReasonServerUnavailable: {StatusUnknown, ActionRetryLater},
		ReasonTemporaryError:    {StatusUnknown, ActionRetryLater},
		ReasonGreylisted:        {StatusUnknown, ActionRetryLater},
		ReasonAntiSpamBlock:     {StatusUnknown, ActionRetryLater},
		ReasonTimeout:           {StatusUnknown, ActionRetryLater},
		ReasonInternalError:     {StatusUnknown, ActionRetryLater},
	}

	// Actions lists every action.
	Actions = []Action{ActionSend, ActionSendWithCaution, ActionRemove, ActionRetryLater}

	// SMTPProviders lists every provider value.
	SMTPProviders = []SMTPProvider{
		SMTPProviderGoogle, SMTPProviderMicrosoft, SMTPProviderYahoo, SMTPProviderApple, SMTPProviderZoho,
		SMTPProviderProton, SMTPProviderFastmail, SMTPProviderGMX, SMTPProviderYandex, SMTPProviderMailru,
		SMTPProviderAmazon, SMTPProviderMimecast, SMTPProviderProofpoint, SMTPProviderBarracuda, SMTPProviderOther,
	}

	// BulkJobStatuses lists every bulk job status.
	BulkJobStatuses = []BulkJobStatus{
		BulkJobStatusAnalyzing, BulkJobStatusReady, BulkJobStatusQueued, BulkJobStatusRunning,
		BulkJobStatusCancelling, BulkJobStatusFinalizing, BulkJobStatusCompleted, BulkJobStatusFailed,
		BulkJobStatusCancelled, BulkJobStatusExpired,
	}

	// TerminalBulkJobStatuses lists the statuses a job never leaves.
	TerminalBulkJobStatuses = []BulkJobStatus{
		BulkJobStatusCompleted, BulkJobStatusFailed, BulkJobStatusCancelled, BulkJobStatusExpired,
	}

	// BulkJobErrorCodes lists every bulk job error code.
	BulkJobErrorCodes = []BulkJobErrorCode{
		BulkJobErrorInvalidCSV, BulkJobErrorTooManyRows, BulkJobErrorEmptyFile, BulkJobErrorEmailColumnNotFound,
		BulkJobErrorUploadMissing, BulkJobErrorPipelineMismatch, BulkJobErrorMissingResults,
		BulkJobErrorInternalError, BulkJobErrorInsufficientCredits, BulkJobErrorUpstreamCapacity,
		BulkJobErrorAccountSuspended, BulkJobErrorBillingHold, BulkJobErrorInvalidKey,
		BulkJobErrorForbiddenScope, BulkJobErrorKeyDailyCap, BulkJobErrorUploadExpired,
	}

	// ErrorCodes lists the API error codes, in the API's order.
	ErrorCodes = []ErrorCode{
		CodeInvalidRequest, CodeUnauthorized, CodeInsufficientCredits, CodeForbidden, CodeForbiddenOrigin,
		CodeNotFound, CodeConflict, CodeLengthRequired, CodePayloadTooLarge, CodeRateLimited, CodeAbuseThrottled,
		CodeKeyDailyCap, CodeInternalError, CodeServiceUnavailable, CodeUpstreamCapacity,
	}

	// ClientErrorCodes lists the client's own error codes.
	ClientErrorCodes = []ErrorCode{CodeConnectionError, CodeTimeout, CodeUnexpectedResponse, CodeAborted}

	// WebhookEvents lists every webhook event type.
	WebhookEvents = []WebhookEventType{
		WebhookBulkCompleted, WebhookBulkFailed, WebhookCreditsLow, WebhookCreditsExpiring, WebhookTest,
	}
)

// IsStatus reports whether s is a known status.
func IsStatus(s string) bool {
	for _, v := range Statuses {
		if string(v) == s {
			return true
		}
	}
	return false
}

// IsReason reports whether s is a known reason.
func IsReason(s string) bool {
	_, ok := ReasonInfo[Reason(s)]
	return ok
}

// SandboxAddress returns the free test address that answers reason, e.g. accepted@sandbox.email-validation.org.
func SandboxAddress(reason Reason) string {
	return string(reason) + "@" + SandboxDomain
}

func isAPIErrorCode(s string) bool {
	for _, c := range ErrorCodes {
		if string(c) == s {
			return true
		}
	}
	return false
}
