# Changelog

All notable changes to the Go SDK. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
SDK uses [Semantic Versioning](https://semver.org/): a new vocabulary value, option or operation is a minor release;
a removed or renamed public symbol is a major one. The SDK targets API v1 (`/v1/…`).

## [1.1.0] - 2026-11-06

Hardening: one behaviour for retries, timeouts, errors, bulk transfers and webhooks across the official SDKs.

### Added

- `WithRequestTimeout(ctx, d)`: a timeout for the calls made with that context, replacing `WithTimeout`; its
  expiry is a `ConnectionError` with code `timeout` (a plain context deadline stays `aborted`).
- `Bulk.Download(ctx, jobID, path, *DownloadOptions)`: streams the result CSV into a file and resumes a partial
  file with `Range: bytes=<size>-` (a `416` with `Content-Range: bytes */<size>` means already complete).
- `HeadersFromMap(map[string]string)` for `VerifyWebhook`.
- `ResultsOptions.RowFilter` (and `DownloadOptions.RowFilter`), a `[]ResultsFilter`, with `ResultsFilterAll` and
  `FilterStatuses(...)`: an empty list, `all` inside a list, and `spamtrap`, `abuse`, `do_not_mail` or `risky` are
  refused before sending. The 1.0.0 `ResultsOptions.Filter []Status` and `FilterAll` (a `Status`) are unchanged and
  behave as before (an empty list means every row); setting `Filter` and `RowFilter` together is refused.

### Fixed

- Transfers (`Bulk.Create` with a `File`, `Bulk.Results`, `Bulk.ResultsStream`) use the timeout as an idle timeout:
  a slow but progressing upload or download is no longer cut after 30 s, and a results stream no longer fails 30 s
  after the request started while the caller reads it. Each stream read waits at most the timeout, and read errors
  are `*Error` (`timeout`, `connection_error` or `aborted`), never a raw `context.DeadlineExceeded`.
- Uploads allow for the kernel's send buffer: while the file is sent, and while its answer is awaited after the last
  byte, the idle bound is `timeout + min(size, 4 MiB) ÷ 128 KiB/s`, so an upload whose buffered tail drains slowly
  is no longer cut after the server has the whole file.
- The SDK bounds every attempt itself, so a transport or response body that ignores the context cannot hang a call.
- `VerifyWebhook` verifies signatures joined with `, ` and refuses a body that is not a JSON object.
- A dead-socket test proves a `POST` or `DELETE` is sent once when the connection dies after the request was read.

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
