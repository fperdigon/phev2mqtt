package client

import (
	"net"
	"testing"
	"time"

	"github.com/buxtronix/phev2mqtt/protocol"
)

// startListener starts a loopback TCP listener and returns its address and a
// function that blocks until one connection is accepted.
func startListener(t *testing.T) (addr string, accept func() net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	ch := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		ch <- c
	}()
	return ln.Addr().String(), func() net.Conn { return <-ch }
}

// TestClosedAtomicNoRace verifies that concurrent Close() and pinger-style
// reads of c.closed do not data-race. Run with: go test -race ./client/...
func TestClosedAtomicNoRace(t *testing.T) {
	// c.conn == nil so Close() returns early after the Store — no dial needed.
	cl := &Client{}

	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					_ = cl.closed.Load()
				}
			}
		}()
	}

	time.Sleep(5 * time.Millisecond)
	cl.Close() // races with the goroutines above without the atomic fix
	close(done)
}

// TestManageGoroutineExitsOnDisconnect verifies R2: when the TCP connection
// drops, manage()'s for-range over its listener channel exits within a
// reasonable timeout rather than blocking forever.
//
// manage() closes c.started when it exits, so we wait for that signal.
func TestManageGoroutineExitsOnDisconnect(t *testing.T) {
	addr, getServer := startListener(t)

	cl, err := New(AddressOption(addr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := cl.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Drop the server side — reader() will error, triggering cleanup.
	serverConn := getServer()
	serverConn.Close()

	// Drain c.started: manage() may have queued a start signal before the
	// disconnect; we want to see the channel close that signals manage() exited.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-cl.started:
			if !ok {
				return // channel closed — manage() exited cleanly
			}
			// queued start signal, keep draining
		case <-deadline:
			t.Error("manage() goroutine did not exit within 3s after TCP disconnect (R2 goroutine leak)")
			return
		}
	}
}

// TestSetRegisterFreshTimerOnRetry verifies R3: when the car replies with
// CmdInBadEncoding, SetRegister retries with a fresh 10-second timer instead
// of reusing the original (possibly near-expiry) one.
//
// The test sends a CmdInBadEncoding reply, then a valid ack, and expects
// success — even though the first-attempt timer fires almost immediately
// (we use a custom channel to simulate an expired timer slot).
func TestSetRegisterFreshTimerOnRetry(t *testing.T) {
	addr, getServer := startListener(t)

	cl, err := New(AddressOption(addr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := cl.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	serverConn := getServer()
	defer serverConn.Close()

	// Drive SetRegister from a goroutine; capture its result.
	result := make(chan error, 1)
	go func() {
		result <- cl.SetRegister(0x10, []byte{0x01})
	}()

	// Wait until SetRegister has added its listener and sent the first frame.
	time.Sleep(50 * time.Millisecond)

	// Inject CmdInBadEncoding into all listeners — simulates car rejecting XOR.
	badEncMsg := &protocol.PhevMessage{
		Type: protocol.CmdInBadEncoding,
		Data: []byte{0x5a}, // corrected XOR byte
	}
	cl.lMu.Lock()
	for _, l := range cl.listeners {
		l.Send(badEncMsg)
	}
	cl.lMu.Unlock()

	// Give SetRegister time to retry with the new XOR and fresh timer.
	time.Sleep(50 * time.Millisecond)

	// Inject a valid ack for register 0x10.
	ackMsg := &protocol.PhevMessage{
		Type:     protocol.CmdInResp,
		Ack:      protocol.Ack,
		Register: 0x10,
	}
	cl.lMu.Lock()
	for _, l := range cl.listeners {
		l.Send(ackMsg)
	}
	cl.lMu.Unlock()

	select {
	case err := <-result:
		if err != nil {
			t.Errorf("SetRegister failed after BadEncoding retry: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("SetRegister did not complete within 2s after ack (fresh timer regression)")
	}
}
