package rubrics

import (
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingWriteCloser never returns from Write until closed, simulating a
// stdin pipe nobody is reading from.
type blockingWriteCloser struct {
	closed atomic.Bool
	done   chan struct{}
}

func newBlockingWriteCloser() *blockingWriteCloser {
	return &blockingWriteCloser{done: make(chan struct{})}
}

func (w *blockingWriteCloser) Write(p []byte) (int, error) {
	<-w.done
	return 0, errors.New("io: read/write on closed pipe")
}

func (w *blockingWriteCloser) Close() error {
	if w.closed.CompareAndSwap(false, true) {
		close(w.done)
	}
	return nil
}

func TestSendToStdin_TimeoutKillsAndResetsOwnedPipe(t *testing.T) {
	t.Parallel()

	origWriter := newBlockingWriteCloser()
	defer origWriter.Close()

	var cleanupCalled atomic.Bool
	p := &Program{
		inputWriter: origWriter,
		inputReader: strings.NewReader(""),
		ownsPipe:    true,
		running:     true,
		cleanup: func() error {
			cleanupCalled.Store(true)
			return nil
		},
	}

	start := time.Now()
	err := p.sendToStdin("hello")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin write timed out")
	assert.GreaterOrEqual(t, elapsed, 750*time.Millisecond)
	assert.False(t, p.running, "timeout should mark the program as not running")
	assert.True(t, cleanupCalled.Load(), "timeout should kill the presumed-wedged process")

	// The pipe should have been replaced, not just left pointing at the
	// still-blocked original writer.
	assert.NotSame(t, origWriter, p.inputWriter)
}

func TestSendToStdin_TimeoutLeavesUnownedPipeAlone(t *testing.T) {
	t.Parallel()

	origWriter := newBlockingWriteCloser()
	defer origWriter.Close()
	origReader := strings.NewReader("")

	p := &Program{
		inputWriter: origWriter,
		inputReader: origReader,
		ownsPipe:    false, // as set by WithReaderWriter
		running:     true,
		cleanup:     func() error { return nil },
	}

	err := p.sendToStdin("hello")
	require.Error(t, err)

	// Callers that supplied their own reader/writer via WithReaderWriter own
	// that lifecycle; resetPipe must not touch it.
	assert.Same(t, origWriter, p.inputWriter)
	assert.Same(t, io.Reader(origReader), p.inputReader)
}

func TestResetPipe(t *testing.T) {
	t.Parallel()

	t.Run("no-op when pipe is not owned", func(t *testing.T) {
		t.Parallel()
		w := newBlockingWriteCloser()
		defer w.Close()
		r := strings.NewReader("")
		p := &Program{inputWriter: w, inputReader: r, ownsPipe: false}

		p.resetPipe()

		assert.Same(t, w, p.inputWriter)
		assert.Same(t, io.Reader(r), p.inputReader)
		assert.False(t, w.closed.Load())
	})

	t.Run("closes and replaces an owned pipe", func(t *testing.T) {
		t.Parallel()
		w := newBlockingWriteCloser()
		r := strings.NewReader("")
		p := &Program{inputWriter: w, inputReader: r, ownsPipe: true}

		p.resetPipe()

		assert.True(t, w.closed.Load(), "old writer should be closed so any stale reader unblocks")
		assert.NotSame(t, w, p.inputWriter)
		assert.NotSame(t, io.Reader(r), p.inputReader)
	})
}

func TestWaitForOutput_WaitsForQuietPeriodAcrossMultipleWrites(t *testing.T) {
	t.Parallel()

	p := &Program{inputWriter: newBlockingWriteCloser()}
	prevOutLen, prevErrLen := p.out.Len(), p.errOut.Len()

	const gap = 15 * time.Millisecond
	go func() {
		time.Sleep(gap)
		_, _ = p.out.Write([]byte("first-chunk"))
		time.Sleep(gap)
		_, _ = p.out.Write([]byte("second-chunk"))
	}()

	start := time.Now()
	p.waitForOutput(prevOutLen, prevErrLen)
	elapsed := time.Since(start)

	assert.Equal(t, "first-chunksecond-chunk", p.out.String()[prevOutLen:])
	// Must have waited past the second write, not returned right after the
	// first chunk showed up.
	assert.GreaterOrEqual(t, elapsed, 2*gap)
}

func TestWaitForOutput_NilInputWriterReturnsImmediately(t *testing.T) {
	t.Parallel()

	p := &Program{}
	start := time.Now()
	p.waitForOutput(0, 0)
	assert.Less(t, time.Since(start), 50*time.Millisecond)
}
