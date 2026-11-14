package webterm

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// terminalPeer implements the server half of framing and bootstrap so tests
// exercise connection setup and WebSockets over net.Pipe without opening ports.
type terminalPeer struct {
	conn   *websocket.Conn
	cipher *aesCipher
}

func (p *terminalPeer) read() (uint16, []byte, error) {
	_, frame, err := p.conn.ReadMessage()
	if err != nil {
		return 0, nil, err
	}
	if len(frame) < outerHeaderSize || int(binary.LittleEndian.Uint32(frame)) != len(frame)-outerHeaderSize {
		return 0, nil, errors.New("invalid request frame")
	}
	plain, err := p.cipher.decrypt(frame[outerHeaderSize:])
	if err != nil {
		return 0, nil, err
	}
	if len(plain) < commandHeaderSize {
		return 0, nil, errors.New("short command")
	}
	return binary.LittleEndian.Uint16(plain[2:]), plain[commandHeaderSize:], nil
}

func (p *terminalPeer) reply(cmd uint16, code uint8, body []byte) error {
	plain := make([]byte, responseHeaderSize+len(body))
	binary.LittleEndian.PutUint16(plain[2:], cmd)
	plain[4] = code
	copy(plain[responseHeaderSize:], body)
	enc, err := p.cipher.encrypt(plain)
	if err != nil {
		return err
	}
	frame := make([]byte, outerHeaderSize+len(enc))
	binary.LittleEndian.PutUint32(frame, uint32(len(enc)))
	copy(frame[outerHeaderSize:], enc)
	return p.conn.WriteMessage(websocket.BinaryMessage, frame)
}

func newTestTerminal(t *testing.T, timeout time.Duration, serve func(*terminalPeer)) *Client {
	t.Helper()
	finished := make(chan struct{})
	upgrader := websocket.Upgrader{}
	serverConn, clientConn := net.Pipe()
	listener := &pipeListener{conn: serverConn, done: make(chan struct{})}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Error(err)
			return
		}
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Error(err)
			return
		}
		key, _ := initialKey()
		cipher, _ := newAES(key)
		p := &terminalPeer{conn: conn, cipher: cipher}
		cmd, token, err := p.read()
		if err != nil || cmd != cmdBootstrap || len(token) != 64 {
			t.Errorf("bootstrap: command %d, token length %d, error %v", cmd, len(token), err)
			return
		}
		sessionKey := bytes.Repeat([]byte{7}, 32)
		body := make([]byte, 66)
		binary.LittleEndian.PutUint16(body, 6316)
		if err := p.reply(cmd, 0, append(body, sessionKey...)); err != nil {
			t.Error(err)
			return
		}
		p.cipher, _ = newAES(sessionKey)
		serve(p)
	})}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("test HTTP server: %v", err)
		}
	}()
	t.Cleanup(func() { _ = server.Close(); _ = clientConn.Close(); _ = serverConn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dialer := &websocket.Dialer{NetDialContext: func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }}
	c, err := dial(ctx, Options{URL: "ws://terminal.test/terminal", Timeout: timeout}, dialer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		select {
		case <-finished:
		case <-time.After(6 * time.Second):
			t.Error("terminal handler did not stop")
		}
	})
	if c.TerminalBuild() != 6316 {
		t.Fatalf("build = %d", c.TerminalBuild())
	}
	return c
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for test event")
		var zero T
		return zero
	}
}

func TestConcurrentCommands(t *testing.T) {
	const count = 32
	c := newTestTerminal(t, 3*time.Second, func(p *terminalPeer) {
		for i := 0; i < count; i++ {
			cmd, body, err := p.read()
			if err != nil {
				t.Error(err)
				return
			}
			if err := p.reply(cmd, 0, body); err != nil {
				t.Error(err)
				return
			}
		}
		// Keep the connection open until client cleanup.
		_, _, _ = p.read()
	})
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			payload := []byte{byte(i)}
			resp, err := c.call(context.Background(), cmdPing, payload)
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(resp.body, payload) {
				t.Errorf("reply %v, want %v", resp.body, payload)
			}
		}(i)
	}
	close(start)
	wg.Wait()
}

func TestQueuedCancellationKeepsConnection(t *testing.T) {
	arrived := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	c := newTestTerminal(t, 3*time.Second, func(p *terminalPeer) {
		cmd, _, err := p.read()
		if err != nil {
			t.Error(err)
			return
		}
		close(arrived)
		select {
		case <-release:
		case <-time.After(4 * time.Second):
			return
		}
		if err := p.reply(cmd, 0, nil); err != nil {
			t.Error(err)
			return
		}
		cmd, _, err = p.read()
		if err != nil {
			t.Error(err)
			return
		}
		if err := p.reply(cmd, 0, nil); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = p.read()
	})
	first := make(chan error, 1)
	go func() { first <- c.Ping(context.Background()) }()
	receive(t, arrived)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued call: %v", err)
	}
	release <- struct{}{}
	if err := receive(t, first); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("connection unusable: %v", err)
	}
}

func TestCanceledContextSendsNothing(t *testing.T) {
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		cmd, _, err := p.read()
		if err != nil {
			t.Error(err)
			return
		}
		if cmd != cmdPing {
			t.Errorf("command = %d", cmd)
		}
		if err := p.reply(cmd, 0, nil); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = p.read()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled call: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptedCommandClosesConnection(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			arrived := make(chan struct{})
			disconnected := make(chan error, 1)
			timeout := 3 * time.Second
			if mode == "timeout" {
				timeout = 100 * time.Millisecond
			}
			c := newTestTerminal(t, timeout, func(p *terminalPeer) {
				_, _, err := p.read()
				if err != nil {
					t.Error(err)
					return
				}
				close(arrived)
				_, _, err = p.read()
				disconnected <- err
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- c.Ping(ctx) }()
			receive(t, arrived)
			want := context.DeadlineExceeded
			if mode == "cancel" {
				want = context.Canceled
				cancel()
			}
			if err := receive(t, result); !errors.Is(err, want) {
				t.Fatalf("interrupted call: %v, want %v", err, want)
			}
			if err := c.Ping(context.Background()); !errors.Is(err, want) {
				t.Fatalf("reused connection: %v", err)
			}
			if err := receive(t, disconnected); err == nil {
				t.Fatal("another request reached the socket after interruption")
			}
		})
	}
}

func TestCloseWakesActiveAndQueuedCalls(t *testing.T) {
	arrived := make(chan struct{})
	c := newTestTerminal(t, 3*time.Second, func(p *terminalPeer) {
		if _, _, err := p.read(); err != nil {
			t.Error(err)
			return
		}
		close(arrived)
		_, _, _ = p.read()
	})
	results := make(chan error, 2)
	go func() { results <- c.Ping(context.Background()) }()
	receive(t, arrived)
	go func() { results <- c.Ping(context.Background()) }()
	_ = c.Close()
	_ = c.Close()
	for i := 0; i < 2; i++ {
		if err := receive(t, results); !errors.Is(err, ErrClosed) {
			t.Fatalf("close: %v", err)
		}
	}
}

func TestDisconnectWakesCaller(t *testing.T) {
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) { _, _, _ = p.read() })
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("expected disconnect error")
	}
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("disconnected client accepted another command")
	}
}

func TestStatusErrors(t *testing.T) {
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		for {
			cmd, _, err := p.read()
			if err != nil {
				return
			}
			code := uint8(9)
			if cmd == cmdLogin {
				code = 3
			}
			if err := p.reply(cmd, code, nil); err != nil {
				t.Error(err)
				return
			}
		}
	})
	tests := []struct {
		name string
		cmd  uint16
		code uint8
		run  func() error
	}{
		{"login", cmdLogin, 3, func() error { _, err := c.Login(context.Background(), LoginOptions{Login: 1}); return err }},
		{"account", cmdAccount, 9, func() error { _, err := c.Account(context.Background()); return err }},
		{"history", cmdTradeHistory, 9, func() error { _, err := c.History(context.Background(), time.Time{}, time.Time{}); return err }},
		{"ping", cmdPing, 9, func() error { return c.Ping(context.Background()) }},
		{"logout", cmdLogout, 9, func() error { return c.Logout(context.Background()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("operation: %w", tt.run())
			var status *StatusError
			if !errors.As(err, &status) || status.Command != tt.cmd || status.Code != tt.code {
				t.Fatalf("unexpected status error: %v", err)
			}
		})
	}
}

// pipeListener lets net/http perform a real WebSocket upgrade over net.Pipe.
type pipeListener struct {
	conn     net.Conn
	done     chan struct{}
	once     sync.Once
	accepted bool
}

func (l *pipeListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return l.conn.LocalAddr() }

func TestCancellationInterruptsBlockedWrite(t *testing.T) {
	ready := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		close(ready)
		// net.Pipe has no buffering: without a reader the client's write blocks.
		select {
		case <-release:
		case <-time.After(4 * time.Second):
			return
		}
		_, _, _ = p.read()
	})
	receive(t, ready)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := c.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write: %v", err)
	}
	if err := c.Ping(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("connection reused after failed write: %v", err)
	}
	release <- struct{}{}
}

func TestMalformedReplyClosesConnection(t *testing.T) {
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		if _, _, err := p.read(); err != nil {
			t.Error(err)
			return
		}
		if err := p.conn.WriteMessage(websocket.BinaryMessage, []byte{1}); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = p.read()
	})
	if err := c.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "short frame") {
		t.Fatalf("malformed reply: %v", err)
	}
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("malformed connection was reused")
	}
}
