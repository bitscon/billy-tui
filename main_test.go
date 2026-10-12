package main

import (
	"net"
	"path/filepath"
	"testing"
)

func TestDefaultAddrPrefersTheSocketWhenItExists(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "billy.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if got := defaultAddr(sock); got != "unix://"+sock {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultAddrFallsBackToTheLoopbackWithoutASocket(t *testing.T) {
	if got := defaultAddr(filepath.Join(t.TempDir(), "absent.sock")); got != "http://localhost:5001" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultAddrIgnoresARegularFile(t *testing.T) {
	if got := defaultAddr("main.go"); got != "http://localhost:5001" {
		t.Fatalf("got %q", got)
	}
}
