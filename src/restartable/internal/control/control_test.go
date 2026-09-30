package control

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
		req.Respond(Response{OK: true, ID: "unit-id", State: "running", PID: &pid, Generation: 9})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := Call(ctx, path, Status)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.ID != "unit-id" || response.State != "running" || response.PID == nil || *response.PID != 412 || response.Generation != 9 {
		t.Fatalf("response = %+v", response)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket still exists after Close: %v", err)
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

	tooLong := filepath.Join(string(os.PathSeparator), strings.Repeat("x", MaxSocketPath))
	if err := ValidatePath(tooLong); err == nil {
		t.Fatalf("accepted %d byte path", len(tooLong))
	}
	if err := ValidatePath(strings.Repeat("x", MaxSocketPath)); err != nil {
		t.Fatalf("rejected exactly %d byte path: %v", MaxSocketPath, err)
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
