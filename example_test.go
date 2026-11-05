package emailvalidation_test

// These examples are compiled by `go test` (they have no Output comment, so they do not run: they would need a real
// key). The README and the docs page use the same code.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"email-validation.org/go/emailvalidation"
)

func Example() {
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	result, err := client.Validate(ctx, "accepted@sandbox.email-validation.org", nil) // free test address
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Status, result.Reason, result.Action)
}

func ExampleClient_Validate() {
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"), emailvalidation.WithTimeout(20*time.Second))
	if err != nil {
		log.Fatal(err)
	}
	result, err := client.Validate(context.Background(), "accepted@sandbox.email-validation.org", &emailvalidation.ValidateOptions{
		IPAddress:      "203.0.113.7",
		TimeoutSeconds: 10,
		Fresh:          emailvalidation.Bool(true),
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
	if result.DidYouMean != nil {
		fmt.Println("did you mean", *result.DidYouMean)
	}
}

func Example_errors() {
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.Validate(context.Background(), "accepted@sandbox.email-validation.org", nil)
	var apiErr *emailvalidation.Error
	switch {
	case err == nil:
		fmt.Println("ok")
	case errors.Is(err, emailvalidation.ErrInsufficientCredits):
		fmt.Println("buy credits")
	case errors.Is(err, emailvalidation.ErrRateLimited):
		if errors.As(err, &apiErr) && apiErr.RetryAfter != nil {
			fmt.Println("retry in", *apiErr.RetryAfter, "s")
		}
	case errors.As(err, &apiErr):
		fmt.Println(apiErr.Kind, apiErr.Status, apiErr.Code, apiErr.RequestID)
	default:
		log.Fatal(err)
	}
}

func ExampleClient_Credits() {
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	credits, err := client.Credits(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("balance", credits.Balance)
	for _, lot := range credits.Lots {
		fmt.Println(lot.Remaining, "expire at", lot.ExpiresAt)
	}

	usage, err := client.Usage(ctx, &emailvalidation.UsageParams{From: "2026-10-01", To: "2026-10-20", GroupBy: emailvalidation.GroupByKey})
	if err != nil {
		log.Fatal(err)
	}
	for _, key := range usage.Keys {
		fmt.Println(key.KeyID, key.CreditsUsed)
	}

	status, err := client.Status(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("service", status.Status, "api", status.Components.API)
}

func ExampleBulkService() {
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	job, err := client.Bulk.Create(ctx, &emailvalidation.BulkCreateParams{
		Emails: []string{"accepted@sandbox.email-validation.org", "no_mailbox@sandbox.email-validation.org"},
	})
	if err != nil {
		log.Fatal(err)
	}
	job, err = client.Bulk.Wait(ctx, job.ID, &emailvalidation.WaitOptions{
		Interval:   5 * time.Second,
		Timeout:    30 * time.Minute,
		OnProgress: func(j *emailvalidation.BulkJob) { fmt.Println(j.Status, j.Processed) },
	})
	if err != nil {
		log.Fatal(err)
	}
	if job.Status != emailvalidation.BulkJobStatusCompleted {
		log.Fatalf("job ended %s", job.Status)
	}
	csv, err := client.Bulk.Results(ctx, job.ID, &emailvalidation.ResultsOptions{
		Filter: []emailvalidation.Status{emailvalidation.StatusValid},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(csv))
}

func ExampleBulkService_Create_file() {
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	f, err := os.Open("contacts.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	job, err := client.Bulk.Create(context.Background(), &emailvalidation.BulkCreateParams{
		File:        f,
		ContentType: emailvalidation.ContentTypeCSV,
		ColumnName:  "email",
		HasHeader:   emailvalidation.Bool(true),
		Start:       emailvalidation.Bool(false), // call Start before Wait
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := client.Bulk.Start(context.Background(), job.ID); err != nil {
		log.Fatal(err)
	}
}

func ExampleBulkService_ResultsStream() {
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"), emailvalidation.WithTimeout(10*time.Minute))
	if err != nil {
		log.Fatal(err)
	}
	stream, err := client.Bulk.ResultsStream(context.Background(), "job_3Fv8QpL2xM7nC1rT9kW4yB", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	out, err := os.Create("results.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, stream); err != nil {
		log.Fatal(err)
	}
}

func ExampleVerifyWebhook() {
	secret := os.Getenv("EV_WEBHOOK_SECRET") // whsec_…
	http.HandleFunc("/webhooks/email-validation", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body) // the raw body, before any JSON parsing
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		event, err := emailvalidation.VerifyWebhook(body, r.Header, secret, nil)
		if err != nil {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}
		// Deduplicate on r.Header.Get("webhook-id"): retries reuse it.
		switch event.Type {
		case emailvalidation.WebhookBulkCompleted, emailvalidation.WebhookBulkFailed:
			data, err := event.BulkData()
			if err == nil {
				fmt.Println("job", data.JobID, data.Status)
			}
		case emailvalidation.WebhookCreditsLow:
			data, err := event.CreditsLowData()
			if err == nil {
				fmt.Println("balance", data.Balance)
			}
		case emailvalidation.WebhookTest:
			// "Send test" in the dashboard
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// loggingTransport wraps the default transport. Request's string form hides the key and the body.
type loggingTransport struct {
	next emailvalidation.Transport
}

func (t loggingTransport) Do(ctx context.Context, req *emailvalidation.Request) (*emailvalidation.Response, error) {
	log.Println("sending", req) // e.g. Request{POST https://api.email-validation.org/v1/validate, headers and body hidden}
	return t.next.Do(ctx, req)
}

func ExampleWithTransport() {
	proxied := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	client, err := emailvalidation.New(os.Getenv("EV_API_KEY"),
		emailvalidation.WithTransport(loggingTransport{next: emailvalidation.NewHTTPTransport(proxied)}))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
}
