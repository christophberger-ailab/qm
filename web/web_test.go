package web

import (
	"net"
	"strconv"
	"testing"
)

// A taken port is skipped when searching, and is an error when the address
// was asked for by name.
func TestListenCountsUpPastATakenPort(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	addr := taken.Addr().String()
	port := taken.Addr().(*net.TCPAddr).Port

	if ln, err := listen(addr, false); err == nil {
		ln.Close()
		t.Fatalf("listen(%s, false) succeeded on a taken port", addr)
	}

	ln, err := listen(addr, true)
	if err != nil {
		t.Fatalf("listen(%s, true): %v", addr, err)
	}
	defer ln.Close()
	got := ln.Addr().(*net.TCPAddr).Port
	if got <= port || got >= port+maxPortTries {
		t.Errorf("got port %d, want one above %d", got, port)
	}
	if _, p, _ := net.SplitHostPort(ln.Addr().String()); p != strconv.Itoa(got) {
		t.Errorf("address %s does not name port %d", ln.Addr(), got)
	}
}
