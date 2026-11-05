package emailvalidation

import (
	"encoding/json"
	"fmt"
)

// The models mirror the API's JSON. Field tags are the API's names, nullable fields are pointers (nil = JSON null)
// and encoding a model with encoding/json reproduces the API's JSON, null fields included. Unknown fields are
// ignored, and an unknown status, reason, action, provider or job status keeps its raw string.
// Times are kept as the API's exact ISO 8601 UTC strings ("2026-10-20T09:30:00.000Z"); dates as "YYYY-MM-DD".

// ValidationResult is the result of one check. Fields are in the API's order.
type ValidationResult struct {
	// Email is the normalised address.
	Email          string        `json:"email"`
	Status         Status        `json:"status"`
	Reason         Reason        `json:"reason"`
	Action         Action        `json:"action"`
	Account        *string       `json:"account"`
	Domain         *string       `json:"domain"`
	DidYouMean     *string       `json:"did_you_mean"`
	FreeEmail      *bool         `json:"free_email"`
	RoleAccount    *bool         `json:"role_account"`
	Disposable     *bool         `json:"disposable"`
	CatchAllDomain *bool         `json:"catch_all_domain"`
	MXFound        *bool         `json:"mx_found"`
	MXRecord       *string       `json:"mx_record"`
	SMTPProvider   *SMTPProvider `json:"smtp_provider"`
	DomainAgeDays  *int64        `json:"domain_age_days"`
	// ProcessedAt is when this result was produced. A reused result can come from a check made up to 12 months
	// earlier; set Fresh for a new check.
	ProcessedAt string `json:"processed_at"`
	// CreditsUsed is 1 when the result cost a credit; unknown, malformed and sandbox answers are free (0).
	CreditsUsed int64 `json:"credits_used"`
	// RequestID is our id for the request; quote it to support.
	RequestID string `json:"request_id"`
}

// CreditLot is a batch of credits that expires at one time.
type CreditLot struct {
	Remaining int64  `json:"remaining"`
	ExpiresAt string `json:"expires_at"`
}

// Credits is the account's credit balance.
type Credits struct {
	Balance int64 `json:"balance"`
	Debt    int64 `json:"debt"`
	Pending int64 `json:"pending"`
	// Lots are soonest-expiring first (the order credits are spent in).
	Lots []CreditLot `json:"lots"`
}

// StatusCounts counts results by status.
type StatusCounts struct {
	Valid      int64 `json:"valid"`
	Invalid    int64 `json:"invalid"`
	CatchAll   int64 `json:"catch_all"`
	Unknown    int64 `json:"unknown"`
	Disposable int64 `json:"disposable"`
}

// UsageTotals sums a usage period.
type UsageTotals struct {
	CreditsUsed int64        `json:"credits_used"`
	ByStatus    StatusCounts `json:"by_status"`
}

// UsageDay is one day of usage.
type UsageDay struct {
	Date        string `json:"date"`
	CreditsUsed int64  `json:"credits_used"`
	// ByStatus is approximate.
	ByStatus StatusCounts `json:"by_status"`
}

// UsageByKey is the usage of one API key.
type UsageByKey struct {
	KeyID string `json:"key_id"`
	// Name is nil for a deleted key.
	Name        *string `json:"name"`
	Last4       *string `json:"last4"`
	CreditsUsed int64   `json:"credits_used"`
}

// UsageResponse is credit usage for a period. The API's items are in Days when GroupBy is "day" and in Keys when
// GroupBy is "key".
type UsageResponse struct {
	From    string
	To      string
	GroupBy UsageGroupBy
	Totals  UsageTotals
	Days    []UsageDay
	Keys    []UsageByKey

	rawItems json.RawMessage // items of an unknown grouping
}

type usageWire struct {
	From    string          `json:"from"`
	To      string          `json:"to"`
	GroupBy UsageGroupBy    `json:"group_by"`
	Totals  UsageTotals     `json:"totals"`
	Items   json.RawMessage `json:"items"`
}

// UnmarshalJSON decodes the API's usage JSON, choosing the item type by group_by.
func (u *UsageResponse) UnmarshalJSON(data []byte) error {
	var w usageWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*u = UsageResponse{From: w.From, To: w.To, GroupBy: w.GroupBy, Totals: w.Totals}
	if len(w.Items) == 0 || string(w.Items) == "null" {
		return nil
	}
	switch w.GroupBy {
	case GroupByDay:
		return json.Unmarshal(w.Items, &u.Days)
	case GroupByKey:
		return json.Unmarshal(w.Items, &u.Keys)
	default:
		u.rawItems = append(json.RawMessage(nil), w.Items...)
		return nil
	}
}

// MarshalJSON encodes the usage back to the API's JSON.
func (u UsageResponse) MarshalJSON() ([]byte, error) {
	w := usageWire{From: u.From, To: u.To, GroupBy: u.GroupBy, Totals: u.Totals}
	var err error
	switch {
	case u.GroupBy == GroupByDay:
		w.Items, err = json.Marshal(nonNil(u.Days))
	case u.GroupBy == GroupByKey:
		w.Items, err = json.Marshal(nonNil(u.Keys))
	case u.rawItems != nil:
		w.Items = u.rawItems
	default:
		w.Items = json.RawMessage("[]")
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(w)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// BulkJob is a bulk validation job. Fields are in the API's order.
type BulkJob struct {
	ID       string        `json:"id"`
	Status   BulkJobStatus `json:"status"`
	Filename *string       `json:"filename"`
	// TotalRows, UniqueEmails, Duplicates, SyntaxInvalid and LocalAnswered are nil while the job is analyzing.
	TotalRows       *int64 `json:"total_rows"`
	UniqueEmails    *int64 `json:"unique_emails"`
	Duplicates      *int64 `json:"duplicates"`
	SyntaxInvalid   *int64 `json:"syntax_invalid"`
	LocalAnswered   *int64 `json:"local_answered"`
	Processed       int64  `json:"processed"`
	CreditsReserved int64  `json:"credits_reserved"`
	CreditsCharged  int64  `json:"credits_charged"`
	// Counts holds the statuses counted so far; a status not yet seen is absent.
	Counts map[Status]int64 `json:"counts"`
	// ErrorCode says why a job failed or waits in ready (BulkJobErrorCodes); nil otherwise.
	ErrorCode         *BulkJobErrorCode `json:"error_code"`
	WebhookEndpointID *string           `json:"webhook_endpoint_id"`
	CreatedAt         string            `json:"created_at"`
	StartedAt         *string           `json:"started_at"`
	FinishedAt        *string           `json:"finished_at"`
	ExpiresAt         string            `json:"expires_at"`
}

// IsTerminal reports whether the job is completed, failed, cancelled or expired.
func (j *BulkJob) IsTerminal() bool { return j.Status.IsTerminal() }

// isStuckReady reports a job left in ready because its start was refused: it never moves on by itself.
func (j *BulkJob) isStuckReady() bool {
	return j.Status == BulkJobStatusReady && j.ErrorCode != nil && *j.ErrorCode != ""
}

// ServiceComponents is the state of each part of the service.
type ServiceComponents struct {
	Website  ComponentState `json:"website"`
	API      ComponentState `json:"api"`
	Bulk     ComponentState `json:"bulk"`
	SignIn   ComponentState `json:"sign_in"`
	Payments ComponentState `json:"payments"`
	Support  ComponentState `json:"support"`
}

// ServiceStatus is the public service status.
type ServiceStatus struct {
	Status     ComponentState    `json:"status"`
	Components ServiceComponents `json:"components"`
	UpdatedAt  string            `json:"updated_at"`
}

// WebhookEvent is a verified webhook delivery. Decode Data with the accessor for its Type.
type WebhookEvent struct {
	Type      WebhookEventType `json:"type"`
	Timestamp string           `json:"timestamp"`
	Data      json.RawMessage  `json:"data"`
}

// BulkWebhookData is the data of bulk.completed and bulk.failed events.
type BulkWebhookData struct {
	JobID          string            `json:"job_id"`
	Status         BulkJobStatus     `json:"status"`
	TotalRows      *int64            `json:"total_rows"`
	UniqueEmails   *int64            `json:"unique_emails"`
	Processed      int64             `json:"processed"`
	CreditsCharged int64             `json:"credits_charged"`
	Counts         map[Status]int64  `json:"counts"`
	ErrorCode      *BulkJobErrorCode `json:"error_code"`
}

// CreditsLowData is the data of credits.low events.
type CreditsLowData struct {
	Balance   int64 `json:"balance"`
	Threshold int64 `json:"threshold"`
}

// CreditsExpiringData is the data of credits.expiring events.
type CreditsExpiringData struct {
	Credits   int64  `json:"credits"`
	ExpiresAt string `json:"expires_at"`
}

// WebhookTestData is the data of webhook.test events.
type WebhookTestData struct {
	EndpointID string `json:"endpoint_id"`
}

func decodeEventData[T any](e *WebhookEvent, want ...WebhookEventType) (*T, error) {
	ok := false
	for _, t := range want {
		ok = ok || e.Type == t
	}
	if !ok {
		return nil, fmt.Errorf("emailvalidation: event type is %q, not %q", e.Type, want[0])
	}
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// BulkData decodes the data of a bulk.completed or bulk.failed event.
func (e *WebhookEvent) BulkData() (*BulkWebhookData, error) {
	return decodeEventData[BulkWebhookData](e, WebhookBulkCompleted, WebhookBulkFailed)
}

// CreditsLowData decodes the data of a credits.low event.
func (e *WebhookEvent) CreditsLowData() (*CreditsLowData, error) {
	return decodeEventData[CreditsLowData](e, WebhookCreditsLow)
}

// CreditsExpiringData decodes the data of a credits.expiring event.
func (e *WebhookEvent) CreditsExpiringData() (*CreditsExpiringData, error) {
	return decodeEventData[CreditsExpiringData](e, WebhookCreditsExpiring)
}

// TestData decodes the data of a webhook.test event.
func (e *WebhookEvent) TestData() (*WebhookTestData, error) {
	return decodeEventData[WebhookTestData](e, WebhookTest)
}
