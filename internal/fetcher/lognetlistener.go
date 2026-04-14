package fetcher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// logBatchFlushInterval is the maximum time the listener buffers parsed
// entries before handing them to the consumer. Short enough to feel live in
// the TUI, long enough to amortize channel ops under high RPS.
const logBatchFlushInterval = 100 * time.Millisecond

// logBatchSize is the batch size at which the listener flushes early without
// waiting for the timer.
const logBatchSize = 32

// maxLogLineBytes caps the maximum length of a single log line read from the
// TCP connection. Caddy access logs are typically a few hundred bytes; the
// limit prevents an unbounded allocation if the remote sends data without a
// newline terminator (e.g. a mis-configured writer or a fuzzing attempt).
const maxLogLineBytes = 1 << 20 // 1 MiB

// LogNetListener accepts TCP connections from Caddy's `net` log writer and
// forwards each parsed line to onBatch. One Ember side, no auth: the listener
// is intended to bind on loopback or an explicitly-chosen address.
//
// Caddy connects once and keeps the connection open, sending one JSON object
// per line. The listener tolerates disconnects and accepts re-connects.
type LogNetListener struct {
	ln      net.Listener
	addr    string
	wg      sync.WaitGroup
	mu      sync.Mutex
	closed  bool
	done    chan struct{} // closed by Close() to unblock watchdogs even when ctx is not cancelled
	lastErr error
}

// NewLogNetListener binds a TCP listener at addr (e.g. "127.0.0.1:0" for a
// random local port). Returns an error if the bind fails.
func NewLogNetListener(addr string) (*LogNetListener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("log listener: %w", err)
	}
	return &LogNetListener{
		ln:   ln,
		addr: ln.Addr().String(),
		done: make(chan struct{}),
	}, nil
}

// Addr returns the bound listener address (host:port) so callers can pass it
// to Caddy when registering the writer.
func (l *LogNetListener) Addr() string {
	return l.addr
}

// LastError returns the most recent non-fatal error encountered by the
// listener (e.g. a malformed JSON line or a dropped connection).
func (l *LogNetListener) LastError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastErr
}

func (l *LogNetListener) setErr(err error) {
	l.mu.Lock()
	l.lastErr = err
	l.mu.Unlock()
}

// Start blocks until ctx is cancelled or Close() is called. onBatch is
// invoked from the connection goroutine; it must return quickly. The slice
// passed to onBatch is reused after the callback returns: callers that need
// to retain entries must copy them before returning.
func (l *LogNetListener) Start(ctx context.Context, onBatch func([]LogEntry)) {
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		select {
		case <-ctx.Done():
		case <-l.done:
		}
		l.Close()
	}()
	// Wait for the watchdog to exit so Close() has returned before Start
	// returns, preventing a leaked goroutine when the caller uses Close
	// without cancelling ctx.
	defer func() { <-watchdogDone }()

acceptLoop:
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed {
				break
			}
			l.setErr(err)
			// Brief pause to avoid a tight loop on repeated accept errors,
			// while still bailing out promptly when the caller cancels ctx
			// or invokes Close: the backoff must not delay shutdown.
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				break acceptLoop
			case <-l.done:
				timer.Stop()
				break acceptLoop
			case <-timer.C:
			}
			continue
		}
		l.wg.Add(1)
		go l.handle(ctx, conn, onBatch)
	}
	l.wg.Wait()
}

// Close stops accepting new connections; in-flight connections are closed
// when their context is cancelled or the remote disconnects.
func (l *LogNetListener) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	close(l.done)
	l.mu.Unlock()
	_ = l.ln.Close()
}

func (l *LogNetListener) handle(ctx context.Context, conn net.Conn, onBatch func([]LogEntry)) {
	defer l.wg.Done()
	defer func() { _ = conn.Close() }()

	handleDone := make(chan struct{})
	defer close(handleDone)
	go func() {
		select {
		case <-ctx.Done():
		case <-l.done:
		case <-handleDone:
		}
		_ = conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLogLineBytes)
	batch := make([]LogEntry, 0, logBatchSize)
	flushTimer := time.NewTimer(logBatchFlushInterval)
	defer flushTimer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		onBatch(batch)
		batch = batch[:0]
	}

	lineCh := make(chan string, 64)
	doneCh := make(chan error, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			select {
			case lineCh <- line:
			case <-ctx.Done():
				return
			case <-l.done:
				return
			}
		}
		err := scanner.Err()
		select {
		case doneCh <- err:
		case <-ctx.Done():
		case <-l.done:
		}
	}()

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case <-l.done:
			flush()
			return
		case line := <-lineCh:
			batch = append(batch, ParseLogLine(line))
			if len(batch) >= logBatchSize {
				flush()
				if !flushTimer.Stop() {
					select {
					case <-flushTimer.C:
					default:
					}
				}
				flushTimer.Reset(logBatchFlushInterval)
			}
		case <-flushTimer.C:
			flush()
			flushTimer.Reset(logBatchFlushInterval)
		case err := <-doneCh:
			flush()
			if err != nil && !errors.Is(err, net.ErrClosed) {
				l.setErr(err)
			}
			return
		}
	}
}
