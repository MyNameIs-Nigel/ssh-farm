//go:build !windows

package main

import (
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// Exercise the real command's configuration/wiring and graceful stop, rather
// than only constructing Server directly in package tests.
func TestServeStartsAndStops(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	t.Setenv("FARM_LISTEN_HOST", "127.0.0.1")
	t.Setenv("FARM_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("FARM_DB_PATH", filepath.Join(dir, "farm.db"))
	t.Setenv("FARM_HOST_KEY_PATH", filepath.Join(dir, "hostkey"))
	t.Setenv("FARM_ALLOW_GUESTS", "true")
	original := slog.Default()
	defer slog.SetDefault(original)
	stopped := make(chan struct{})
	go func() { runServe(); close(stopped) }()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.SetReadDeadline(time.Now().Add(time.Second))
			b := make([]byte, 256)
			n, _ := conn.Read(b)
			conn.Close()
			if n > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("serve did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// No key is still refused at the backend even when trusted guests are enabled.
	_, err = gossh.Dial("tcp", addr, &gossh.ClientConfig{User: "guest", HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: time.Second})
	if err == nil {
		t.Error("direct anonymous connection accepted")
	}
	process, _ := os.FindProcess(os.Getpid())
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
}
