package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rctl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestListenPermissionsStatusAndDuplicateStart(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "runner.sock")
	server, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}
	lockInfo, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if lockInfo.Mode().Perm() != 0600 {
		t.Fatalf("lock mode = %o, want 600", lockInfo.Mode().Perm())
	}
	if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "already active") {
		t.Fatalf("second Listen error = %v", err)
	}

	go func() {
		req := <-server.Requests
		if req.Command != Status {
			t.Errorf("command = %q", req.Command)
		}
		pid := 412
		req.Respond(Response{OK: true, ID: "unit-id", State: "running", PID: &pid, Generation: 9, Ready: true})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := Call(ctx, path, Status)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.ID != "unit-id" || response.State != "running" || response.PID == nil || *response.PID != 412 || response.Generation != 9 || !response.Ready {
		t.Fatalf("response = %+v", response)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket still exists after Close: %v", err)
	}
}

func TestResponseAlwaysIncludesReadyBoolean(t *testing.T) {
	data, err := json.Marshal(Response{OK: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ready":false`) {
		t.Fatalf("status response omitted ready=false: %s", data)
	}
}

func TestListenRemovesOwnedStaleSocketAndRejectsRegularFile(t *testing.T) {
	dir := shortSocketDir(t)
	path := filepath.Join(dir, "stale.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	server, err := Listen(path)
	if err != nil {
		t.Fatalf("recover stale socket: %v", err)
	}
	_ = server.Close()

	regular := filepath.Join(dir, "not-a-socket")
	if err := os.WriteFile(regular, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(regular); err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("Listen regular file error = %v", err)
	}
	if data, err := os.ReadFile(regular); err != nil || string(data) != "keep" {
		t.Fatalf("regular file changed: %q, %v", data, err)
	}
	if _, err := os.Stat(regular + ".lock"); err != nil {
		t.Fatalf("stable lock file missing after listen failure: %v", err)
	}
}

func TestListenBindFailureKeepsLockInodeStable(t *testing.T) {
	dir := shortSocketDir(t)
	path := filepath.Join(dir, "runner.sock")
	original := listenControlSocket
	listenControlSocket = func(string) (net.Listener, error) {
		return nil, errors.New("simulated bind failure")
	}
	defer func() { listenControlSocket = original }()
	if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "simulated bind failure") {
		t.Fatalf("Listen error = %v, want simulated bind failure", err)
	}
	lockPath := path + ".lock"
	firstInfo, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("lock file missing after bind failure: %v", err)
	}

	listenControlSocket = original
	server, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen after simulated bind failure: %v", err)
	}
	defer func() { _ = server.Close() }()
	secondInfo, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(firstInfo, secondInfo) {
		t.Fatal("lock inode changed between Listen attempts")
	}
}

func TestInvalidRequestAndPathLimit(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "control.sock")
	server, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(conn, `{"command":"restart-now"}`); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if response.OK || response.Reason != "invalid request" {
		t.Fatalf("invalid response = %+v", response)
	}

	tooLong := strings.Repeat("x", 104)
	if err := ValidatePath(tooLong); err == nil {
		t.Fatalf("accepted 104 byte path")
	}
	if err := ValidatePath(strings.Repeat("x", 103)); err != nil {
		t.Fatalf("rejected exactly 103 byte path: %v", err)
	}
}

func TestExtraNULByteDisconnectsControlRequest(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "control.sock")
	server, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := fmt.Fprintln(conn, `{"command":"restart"}`); err != nil {
		t.Fatal(err)
	}
	request := <-server.Requests
	if _, err := conn.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var data [1]byte
	if n, err := conn.Read(data[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("connection after extra NUL = (%d, %v), want EOF", n, err)
	}
	if request.Alive() {
		t.Fatal("NUL byte after request left the request alive")
	}
}

func TestOversizedControlLineIsRejected(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "control.sock")
	server, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn, `{"command":"status","padding":"%s"}`+"\n", strings.Repeat("x", MaxLineBytes)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	_ = conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "invalid request") {
		t.Fatalf("response line = %q", line)
	}
}

// Close と接続の処理が重なっても panic しない (Requests を閉じると、送信待ちの serveConn が閉じたチャネルへ送って panic した)。
// 受け手 (actor) がいない状態で要求を送らせ、送信待ちの serveConn がある間に Close する。
func TestCloseWhileRequestsArePendingDoesNotPanic(t *testing.T) {
	for range 50 {
		path := filepath.Join(shortSocketDir(t), "runner.sock")
		server, err := Listen(path)
		if err != nil {
			t.Fatal(err)
		}
		conns := make([]net.Conn, 0, 8)
		for range 8 {
			conn, err := net.Dial("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write([]byte(`{"command":"status"}` + "\n")); err != nil {
				t.Fatal(err)
			}
			conns = append(conns, conn)
		}
		// 誰も Requests を読まないので、serveConn は送信で待っている (はず)。その間に閉じる。
		if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatal(err)
		}
		for _, conn := range conns {
			_ = conn.Close()
		}
	}
}
