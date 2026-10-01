// Package control implements the private Unix socket used to inspect and
// restart a runner.
package control

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	MaxLineBytes  = 4096
	MaxSocketPath = 103
)

var listenControlSocket = func(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}

type Command string

const (
	Status  Command = "status"
	Restart Command = "restart"
)

type Request struct {
	Command Command
	alive   atomic.Bool
	reply   chan Response
	one     sync.Once
}

func (r *Request) Alive() bool { return r.alive.Load() }

func (r *Request) Respond(response Response) {
	r.one.Do(func() { r.reply <- response })
}

type Response struct {
	OK             bool   `json:"ok"`
	ID             string `json:"id,omitempty"`
	State          string `json:"state,omitempty"`
	PID            *int   `json:"pid"`
	Generation     uint64 `json:"generation,omitempty"`
	RestartPending bool   `json:"restartPending,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type requestLine struct {
	Command string `json:"command"`
}

type Server struct {
	path      string
	listener  net.Listener
	lock      *os.File
	sockInfo  os.FileInfo
	Requests  chan *Request
	closeOnce sync.Once
	serveDone chan struct{}
}

func DefaultPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	base := filepath.Join(os.TempDir(), fmt.Sprintf("restartable-%d", os.Getuid()))
	if err := ensurePrivateDir(base); err != nil {
		return "", err
	}
	return filepath.Join(base, hex.EncodeToString(sum[:])[:16]+".sock"), nil
}

func ValidatePath(path string) error {
	if path == "" {
		return errors.New("control socket path is empty")
	}
	if len([]byte(path)) > MaxSocketPath {
		return fmt.Errorf("control socket path is %d bytes; maximum is %d", len([]byte(path)), MaxSocketPath)
	}
	return nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("control directory %q is not private and owned by this user", path)
	}
	return nil
}

// Listen takes an advisory lock before inspecting or removing a stale socket.
func Listen(path string) (*Server, error) {
	if err := ValidatePath(path); err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := ValidatePath(absPath); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0700); err != nil {
		return nil, err
	}
	lockFD, err := syscall.Open(absPath+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open control lock: %w", err)
	}
	lock := os.NewFile(uintptr(lockFD), absPath+".lock")
	lockInfo, err := lock.Stat()
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("inspect control lock: %w", err)
	}
	lockStat, ok := lockInfo.Sys().(*syscall.Stat_t)
	if !ok || int(lockStat.Uid) != os.Getuid() || !lockInfo.Mode().IsRegular() {
		_ = lock.Close()
		return nil, fmt.Errorf("control lock %q is not a regular file owned by this user", absPath+".lock")
	}
	if err := syscall.Fchmod(lockFD, 0600); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("secure control lock: %w", err)
	}
	if err := syscall.Flock(lockFD, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("control socket is already active: %w", err)
	}
	cleanup := func() {
		_ = syscall.Flock(lockFD, syscall.LOCK_UN)
		_ = lock.Close()
	}
	if err := removeStaleSocket(absPath); err != nil {
		cleanup()
		return nil, err
	}
	listener, err := listenControlSocket(absPath)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("listen on control socket: %w", err)
	}
	if err := os.Chmod(absPath, 0600); err != nil {
		_ = listener.Close()
		cleanup()
		_ = os.Remove(absPath)
		return nil, err
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		_ = listener.Close()
		cleanup()
		return nil, err
	}
	s := &Server{path: absPath, listener: listener, lock: lock, sockInfo: info, Requests: make(chan *Request), serveDone: make(chan struct{})}
	go s.acceptLoop()
	return s, nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("control path %q exists and is not a socket", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("stale control socket %q is not owned by this user", path)
	}
	return os.Remove(path)
}

func (s *Server) Path() string { return s.path }

func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.listener.Close()
		<-s.serveDone
		if current, err := os.Lstat(s.path); err == nil && os.SameFile(current, s.sockInfo) {
			_ = os.Remove(s.path)
		}
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		_ = s.lock.Close()
	})
	return closeErr
}

func (s *Server) acceptLoop() {
	defer close(s.serveDone)
	defer close(s.Requests)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	line, err := readLine(conn)
	if err != nil {
		writeError(conn, "invalid request")
		return
	}
	var wire requestLine
	if json.Unmarshal(line, &wire) != nil || (wire.Command != string(Status) && wire.Command != string(Restart)) {
		writeError(conn, "invalid request")
		return
	}
	req := &Request{Command: Command(wire.Command), reply: make(chan Response, 1)}
	req.alive.Store(true)
	disconnected := make(chan struct{})
	// Clients keep the connection open while they await a response. Monitoring
	// the read side lets the actor drop requests disconnected pre-commit.
	go func() {
		var extra [1]byte
		for {
			n, readErr := conn.Read(extra[:])
			if n > 0 || readErr != nil {
				req.alive.Store(false)
				close(disconnected)
				return
			}
		}
	}()
	select {
	case s.Requests <- req:
	case <-disconnected:
		return
	case <-s.serveDone:
		return
	}
	select {
	case response := <-req.reply:
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_ = json.NewEncoder(conn).Encode(response)
	case <-disconnected:
		return
	case <-s.serveDone:
	}
}

func readLine(conn net.Conn) ([]byte, error) {
	r := bufio.NewReaderSize(conn, MaxLineBytes+1)
	line, err := r.ReadSlice('\n')
	if err != nil {
		return nil, err
	}
	if len(line) > MaxLineBytes {
		return nil, errors.New("request too long")
	}
	if r.Buffered() != 0 {
		return nil, errors.New("request contains more than one line")
	}
	return []byte(strings.TrimSuffix(string(line), "\n")), nil
}

func writeError(w io.Writer, reason string) {
	_ = json.NewEncoder(w).Encode(Response{OK: false, Reason: reason})
}

// Call sends one command and waits for a single JSON response.
func Call(ctx context.Context, path string, command Command) (Response, error) {
	if err := ValidatePath(path); err != nil {
		return Response{}, err
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(deadline(ctx))
	if err := json.NewEncoder(conn).Encode(requestLine{Command: string(command)}); err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(bufio.NewReader(io.LimitReader(conn, MaxLineBytes))).Decode(&response); err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, err
	}
	return response, nil
}

func deadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Time{}
}
