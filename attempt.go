package emailvalidation

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// One attempt of a call, with its timeout:
//
//   - JSON calls: the timeout is a deadline on the whole attempt (connect, send, the full response body).
//   - Transfers (a file upload, a results download): the timeout is an idle timeout. The attempt fails when no byte
//     moved for that long: while connecting and uploading, while waiting for the answer, and during each read of the
//     body. There is no total cap, and no timer runs between two reads of a stream (the caller may be busy).
//
// The SDK enforces both bounds itself, so a Transport (or a response body) that ignores ctx cannot hang a call.

// uploadRate is the slowest upload rate assumed when the transport cannot report upload progress: such an upload
// gets timeout + size ÷ 128 KiB/s before the idle rules apply.
const uploadRate = 128 * 1024

// drainCap bounds the drain allowance. Upload progress is counted when net/http hands bytes to the connection, not
// when they leave the machine: megabytes can sit in the socket's send buffer, and while they drain nothing counts.
// So during the upload, and in the wait for the response head after the last byte, the idle bound is
// timeout + min(size, 4 MiB) ÷ 128 KiB/s (at most timeout + 32 s); body reads after the head get timeout again.
const drainCap = 4 << 20

// drainAllowance is min(size, 4 MiB) ÷ 128 KiB/s.
func drainAllowance(size int) time.Duration {
	return time.Duration(float64(min(size, drainCap)) / uploadRate * float64(time.Second))
}

type attempt struct {
	parent context.Context // the caller's context
	ctx    context.Context // the attempt's context, passed to the transport
	cancel context.CancelFunc
	dog    *watchdog // transfers only
}

// newAttempt starts an attempt. For a transfer, the first bound is timeout + uploadAllowance (a transport that cannot
// report upload progress), and each upload progress extends it to timeout + drain from now.
func newAttempt(parent context.Context, long bool, timeout, uploadAllowance, drain time.Duration) *attempt {
	if !long {
		ctx, cancel := context.WithTimeout(parent, timeout)
		return &attempt{parent: parent, ctx: ctx, cancel: cancel}
	}
	ctx, cancel := context.WithCancel(parent)
	a := &attempt{parent: parent, ctx: ctx, cancel: cancel}
	a.dog = newWatchdog(timeout, cancel)
	a.dog.uploadIdle = timeout + drain
	a.dog.arm(timeout + uploadAllowance)
	return a
}

// timedOut reports whether the attempt's own timeout ended it (not the caller).
func (a *attempt) timedOut() bool {
	if a.dog != nil {
		return a.dog.hasFired()
	}
	return a.parent.Err() == nil && errors.Is(a.ctx.Err(), context.DeadlineExceeded)
}

// close ends the attempt: no timer keeps running and the transport's context is done.
func (a *attempt) close() {
	if a.dog != nil {
		a.dog.stop()
		a.dog.timer.Stop()
	}
	a.cancel()
}

// progress is the upload progress hook: each byte handed to the connection extends the bound to timeout + drain, which
// also covers the wait for the response head after the last byte (the send buffer drains meanwhile).
func (a *attempt) progress() {
	if a.dog != nil {
		a.dog.extend()
	}
}

// headersArrived stops the transfer timer: from now on it runs only while a body read is pending.
func (a *attempt) headersArrived() {
	if a.dog != nil {
		a.dog.stop()
	}
}

// do runs the transport, but returns as soon as the attempt's context is done, even if the transport ignores it
// (a late answer is then closed).
func (a *attempt) do(t Transport, req *Request) (*Response, error) {
	type result struct {
		res      *Response
		err      error
		panicked bool
		value    any
	}
	ch := make(chan result, 1)
	go func() {
		r := result{err: &TransportError{Err: errors.New("the transport did not return")}}
		defer func() {
			if v := recover(); v != nil {
				r = result{panicked: true, value: v}
			}
			ch <- r
		}()
		r.res, r.err = t.Do(a.ctx, req)
	}()
	select {
	case r := <-ch:
		if r.panicked {
			panic(r.value) // in the caller's goroutine, as if the transport had been called directly
		}
		return r.res, r.err
	case <-a.ctx.Done():
	}
	// A transport that honours ctx returns at once: wait a little for it, so it is not left running.
	grace := time.NewTimer(transportGrace)
	defer grace.Stop()
	select {
	case r := <-ch:
		if r.panicked {
			panic(r.value)
		}
		if r.res != nil && r.res.Body != nil {
			_ = r.res.Body.Close() // too late: the attempt is over
		}
	case <-grace.C:
		go func() {
			if r := <-ch; r.res != nil && r.res.Body != nil {
				_ = r.res.Body.Close()
			}
		}()
	}
	return nil, &TransportError{Timeout: a.timedOut(), Err: a.ctx.Err()}
}

// transportGrace is how long a cancelled or timed-out attempt waits for the transport to return before leaving it.
const transportGrace = 100 * time.Millisecond

// failure maps a failed attempt (no answer, or a broken body) to the error family.
func (a *attempt) failure(cause error) *Error {
	var apiErr *Error
	if errors.As(cause, &apiErr) {
		return apiErr
	}
	if err := a.parent.Err(); err != nil {
		return abortedError(err)
	}
	if cause == nil {
		cause = a.ctx.Err()
	}
	if a.timedOut() || isTimeout(cause) {
		return &Error{Kind: KindConnection, Code: CodeTimeout, Message: msgTimedOut, Err: cause}
	}
	return &Error{Kind: KindConnection, Code: CodeConnectionError, Message: msgUnreachable, Err: cause}
}

// body wraps a response body so that every read is bounded (the attempt's deadline, or the idle timeout per read),
// read errors are *Error, and closing it ends the attempt.
func (a *attempt) body(b io.ReadCloser) io.ReadCloser {
	if b == nil {
		b = io.NopCloser(eofReader{})
	}
	return &guardedBody{body: b, at: a, reqs: make(chan int), res: make(chan readResult, 1), done: make(chan struct{})}
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

type readResult struct {
	data []byte
	err  error
}

// guardedBody reads the transport's body in a helper goroutine, one read at a time and only when the caller asks
// (pull-based: nothing is buffered ahead), so a read can be abandoned when its bound passes.
type guardedBody struct {
	body io.ReadCloser
	at   *attempt
	err  error // sticky: io.EOF or the *Error of the first failure

	pump      sync.Once
	reqs      chan int
	res       chan readResult
	done      chan struct{}
	closeBody sync.Once
	closeAll  sync.Once
}

func (g *guardedBody) Read(p []byte) (int, error) {
	if g.err != nil {
		return 0, g.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	g.pump.Do(g.startPump)
	if g.at.dog != nil {
		g.at.dog.arm(g.at.dog.idle) // the idle bound runs only while this read waits
		defer g.at.dog.stop()
	}
	select {
	case g.reqs <- len(p):
	case <-g.at.ctx.Done():
		return 0, g.fail(nil)
	}
	select {
	case r := <-g.res:
		n := copy(p, r.data)
		switch {
		case r.err == nil:
			return n, nil
		case errors.Is(r.err, io.EOF):
			g.err = io.EOF
			return n, io.EOF
		default:
			return n, g.fail(r.err)
		}
	case <-g.at.ctx.Done():
		return 0, g.fail(nil)
	}
}

func (g *guardedBody) startPump() {
	go func() {
		var buf []byte
		for {
			select {
			case n := <-g.reqs:
				if cap(buf) < n {
					buf = make([]byte, n)
				}
				m, err := g.body.Read(buf[:n])
				g.res <- readResult{data: buf[:m], err: err}
				if err != nil {
					return
				}
			case <-g.done:
				return
			}
		}
	}()
}

func (g *guardedBody) fail(cause error) error {
	g.err = g.at.failure(cause)
	g.closeBody.Do(func() { _ = g.body.Close() }) // unblocks a read still pending in the transport
	return g.err
}

// Close closes the body and ends the attempt. It is safe to call more than once.
func (g *guardedBody) Close() error {
	var err error
	g.closeBody.Do(func() { err = g.body.Close() })
	g.closeAll.Do(func() {
		close(g.done)
		g.at.close()
	})
	return err
}

// watchdog cancels a transfer attempt when no progress was seen for idle. Progress only moves a deadline; the timer
// re-arms itself lazily when it fires early, so a fast transfer costs one timer per idle period, not one per read.
type watchdog struct {
	idle       time.Duration // the bound of a body read
	uploadIdle time.Duration // the bound after upload progress: idle + the drain allowance
	cancel     context.CancelFunc

	mu     sync.Mutex
	timer  *time.Timer
	at     time.Time // when the timer is due (zero: not scheduled)
	until  time.Time // the current deadline
	active bool
	fired  bool
}

func newWatchdog(idle time.Duration, cancel context.CancelFunc) *watchdog {
	w := &watchdog{idle: idle, uploadIdle: idle, cancel: cancel}
	w.timer = time.AfterFunc(time.Hour, w.fire)
	w.timer.Stop()
	return w
}

// arm starts the bound: the attempt fails unless progress is seen within d.
func (w *watchdog) arm(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fired {
		return
	}
	w.active = true
	w.schedule(time.Now().Add(d))
}

// extend pushes the deadline to uploadIdle from now (never earlier than it is). Only upload progress calls it.
func (w *watchdog) extend() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fired || !w.active {
		return
	}
	if until := time.Now().Add(w.uploadIdle); until.After(w.until) {
		w.until = until // the timer re-arms itself when it fires before this
	}
}

// stop pauses the bound until the next arm.
func (w *watchdog) stop() {
	w.mu.Lock()
	w.active = false
	w.mu.Unlock()
}

func (w *watchdog) hasFired() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fired
}

// schedule sets the deadline; the timer is moved only when it would fire after it. Called with mu held.
func (w *watchdog) schedule(until time.Time) {
	w.until = until
	if w.at.IsZero() || w.at.After(until) {
		w.at = until
		w.timer.Reset(time.Until(until))
	}
}

func (w *watchdog) fire() {
	w.mu.Lock()
	w.at = time.Time{}
	if !w.active || w.fired {
		w.mu.Unlock()
		return
	}
	if d := time.Until(w.until); d > 0 {
		w.at = w.until
		w.timer.Reset(d)
		w.mu.Unlock()
		return
	}
	w.fired = true
	w.mu.Unlock()
	w.cancel()
}
