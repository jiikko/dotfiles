package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
)

type printRequest struct {
	line string
	done chan struct{}
}

// Presenter runs Bubble Tea in its inline mode and adapts its input/output to
// the runner's snapshot and key boundary.
type Presenter struct {
	program  *tea.Program
	keys     chan string
	ready    chan struct{}
	runDone  chan struct{}
	closed   chan struct{}
	prints   chan printRequest
	fallback io.Writer

	startOnce sync.Once
	closeOnce sync.Once
	readyOnce sync.Once
	stateMu   sync.Mutex
	started   bool
	closing   bool
	runErr    error
}

// New creates an inline presenter. It starts only when runner.Run has opened
// its control socket and is ready to own terminal cleanup.
func New(input io.Reader, output io.Writer) *Presenter {
	p := &Presenter{
		keys:     make(chan string, 32),
		ready:    make(chan struct{}),
		runDone:  make(chan struct{}),
		closed:   make(chan struct{}),
		prints:   make(chan printRequest),
		fallback: os.Stderr,
	}
	model := &teaModel{
		state: runner.InitialModel(), width: defaultWidth, keys: p.keys,
		ready: p.markReady, closed: p.closed,
	}
	p.program = tea.NewProgram(model,
		tea.WithInput(input),
		tea.WithOutput(discardWriteErrors{writer: output}),
		tea.WithoutSignalHandler(),
	)
	return p
}

type discardWriteErrors struct{ writer io.Writer }

var _ term.File = discardWriteErrors{}

func (w discardWriteErrors) Write(data []byte) (int, error) {
	_, _ = w.writer.Write(data)
	return len(data), nil
}

func (w discardWriteErrors) Read(data []byte) (int, error) {
	if reader, ok := w.writer.(io.Reader); ok {
		return reader.Read(data)
	}
	return 0, io.EOF
}

func (w discardWriteErrors) Close() error { return nil }

func (w discardWriteErrors) Fd() uintptr {
	if file, ok := w.writer.(interface{ Fd() uintptr }); ok {
		return file.Fd()
	}
	return ^uintptr(0)
}

// Start is called by runner.Run when the configured presenter supports startup.
func (p *Presenter) Start() error {
	p.startOnce.Do(func() {
		p.stateMu.Lock()
		p.started = true
		p.stateMu.Unlock()
		go p.printLoop()
		go p.run()
	})
	select {
	case <-p.ready:
		return nil
	case <-p.runDone:
		if err := p.getRunErr(); err != nil {
			return err
		}
		return errors.New("bubble tea exited before the terminal UI started")
	}
}

func (p *Presenter) run() {
	defer func() {
		if recovered := recover(); recovered != nil {
			p.setRunErr(fmt.Errorf("bubble tea panic: %v", recovered))
		}
		// Run's normal path already shut down the terminal. Kill is idempotent,
		// and also covers startup errors that return before Bubble Tea's loop.
		p.program.Kill()
		p.stateMu.Lock()
		closing := p.closing
		p.stateMu.Unlock()
		if !closing && p.getRunErr() != nil {
			p.emitKey("ctrl+c")
		}
		close(p.runDone)
	}()
	_, err := p.program.Run()
	if err == nil {
		p.stateMu.Lock()
		closing := p.closing
		p.stateMu.Unlock()
		if !closing {
			err = errors.New("bubble tea exited unexpectedly")
		}
	}
	p.setRunErr(err)
}

func (p *Presenter) markReady() { p.readyOnce.Do(func() { close(p.ready) }) }

func (p *Presenter) setRunErr(err error) {
	p.stateMu.Lock()
	p.runErr = err
	p.stateMu.Unlock()
}

func (p *Presenter) getRunErr() error {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.runErr
}

func (p *Presenter) emitKey(key string) {
	select {
	case p.keys <- key:
	case <-p.closed:
	}
}

func (p *Presenter) Render(state runner.Model) {
	select {
	case <-p.runDone:
		return
	case <-p.closed:
		return
	default:
		p.program.Send(snapshotMsg(state))
	}
}

func (p *Presenter) Keys() <-chan string { return p.keys }

// Println is the optional log path used by the runner's bounded TTY log sink.
// A worker owns Program.Println so an unexpected Bubble Tea exit cannot block
// the process actor while it is flushing logs during cleanup.
func (p *Presenter) Println(line string) {
	request := printRequest{line: line, done: make(chan struct{})}
	select {
	case p.prints <- request:
	case <-p.runDone:
		p.writeFallback(line)
		return
	case <-p.closed:
		p.writeFallback(line)
		return
	}
	select {
	case <-request.done:
	case <-p.runDone:
	case <-p.closed:
	}
}

func (p *Presenter) printLoop() {
	for {
		select {
		case request := <-p.prints:
			select {
			case <-p.runDone:
				p.writeFallback(request.line)
				close(request.done)
				return
			case <-p.closed:
				p.writeFallback(request.line)
				close(request.done)
				return
			default:
			}
			printed := make(chan struct{})
			go func(line string) {
				p.program.Println(line)
				close(printed)
			}(request.line)
			select {
			case <-printed:
			case <-p.runDone:
				p.writeFallback(request.line)
			case <-p.closed:
				p.writeFallback(request.line)
			}
			close(request.done)
		case <-p.runDone:
			return
		case <-p.closed:
			return
		}
	}
}

func (p *Presenter) writeFallback(line string) {
	writer := p.fallback
	if writer == nil {
		writer = os.Stderr
	}
	_, _ = fmt.Fprintln(writer, line)
}

func (p *Presenter) Close() error {
	p.closeOnce.Do(func() {
		p.stateMu.Lock()
		p.closing = true
		started := p.started
		p.stateMu.Unlock()
		close(p.closed)
		if started {
			select {
			case <-p.runDone:
			default:
				p.program.Quit()
				<-p.runDone
			}
			close(p.keys)
		}
	})
	return nil
}
