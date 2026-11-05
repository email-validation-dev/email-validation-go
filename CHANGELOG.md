# Changelog

All notable changes to the Go SDK. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
SDK uses [Semantic Versioning](https://semver.org/): a new vocabulary value, option or operation is a minor release;
a removed or renamed public symbol is a major one. The SDK targets API v1 (`/v1/…`).

## [1.0.0] - 2026-11-04

First version, at parity with the JavaScript and Python SDKs.

- `New(apiKey, ...Option)` with `WithBaseURL`, `WithTimeout` (per attempt, default 30 s), `WithMaxRetries`
  (default 2), `WithMaxRetryWait` (default 10 s), `WithHTTPClient` and `WithTransport`.
- `Validate`, `Credits`, `Usage`, `Status`, and `Bulk.Create`, `Get`, `Start`, `Cancel`, `Results`,
  `ResultsStream` and `Wait`; every method takes a `context.Context`.
- Typed models with pointer fields for nullable values; unknown fields and new enum values decode without error.
- One error type, `*Error`, with `Kind`, `Status`, `Code`, `Message`, `RequestID`, `RetryAfter` and per-kind
  sentinels for `errors.Is`; `ErrInvalidArgument` for argument mistakes.
- Retries for 429/503 (all methods, honouring `Retry-After`) and for failed `GET` requests, with jittered
  exponential backoff; redirects are never followed.
- `VerifyWebhook` and `SignWebhook` (Standard Webhooks, HMAC-SHA256, constant-time comparison).
- Vocabulary constants and lists, `IsStatus`, `IsReason`, `SandboxAddress`.
- Standard library only; Go 1.22 or later.
