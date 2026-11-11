package webterm

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

type response struct {
	cmd  uint16
	code uint8
	body []byte
}

type result struct {
	resp *response
	err  error
}

type pendingRequest struct {
	cmd   uint16
	reply chan result
}

// call permits one outstanding command. The protocol has no request IDs, so
// after an interrupted command the connection cannot safely be reused.
func (c *Client) call(ctx context.Context, cmd uint16, payload []byte) (*response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.connectionError()
	}
	// Check again because select may choose the gate for an already canceled call.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch := make(chan result, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.pending = &pendingRequest{cmd: cmd, reply: ch}
	c.mu.Unlock()

	// Closing the socket interrupts blocked writes as well as reads. Cancellation
	// while queued above does not affect the connection or the current command.
	stop := context.AfterFunc(ctx, func() { c.fail(ctx.Err()) })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		c.fail(err)
		return nil, err
	}
	if err := c.writeFrame(cmd, payload); err != nil {
		// A socket deadline can fire just before the context timer runs.
		cause := ctx.Err()
		if cause == nil && !time.Now().Before(deadline) {
			cause = context.DeadlineExceeded
		}
		if cause != nil {
			c.fail(cause)
			return nil, cause
		}
		c.fail(err)
		return nil, c.connectionError()
	}
	select {
	case res := <-ch:
		if ctx.Err() != nil {
			c.fail(ctx.Err())
			return nil, ctx.Err()
		}
		return res.resp, res.err
	case <-ctx.Done():
		c.fail(ctx.Err())
		return nil, ctx.Err()
	}
}

func (c *Client) connectionError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// dispatch ignores unsolicited commands and delivers the active command's reply.
func (c *Client) dispatch(resp *response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil || c.pending.cmd != resp.cmd {
		return
	}
	c.pending.reply <- result{resp: resp}
	c.pending = nil
}

// fail permanently terminates the connection and wakes active and queued calls.
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = fmt.Errorf("webterm: connection closed: %w", err)
	if c.pending != nil {
		c.pending.reply <- result{err: c.err}
		c.pending = nil
	}
	close(c.done)
	c.mu.Unlock()
	c.conn.Close()
}

// writeFrame encrypts and sends one command.
func (c *Client) writeFrame(cmd uint16, payload []byte) error {
	inner := make([]byte, commandHeaderSize+len(payload))
	if _, err := rand.Read(inner[:2]); err != nil {
		return err
	}
	binary.LittleEndian.PutUint16(inner[2:], cmd)
	copy(inner[commandHeaderSize:], payload)

	c.mu.Lock()
	cipher := c.cipher
	c.mu.Unlock()

	enc, err := cipher.encrypt(inner)
	if err != nil {
		return err
	}
	frame := make([]byte, outerHeaderSize+len(enc))
	binary.LittleEndian.PutUint32(frame[0:], uint32(len(enc)))
	binary.LittleEndian.PutUint32(frame[4:], outerVersion)
	copy(frame[outerHeaderSize:], enc)

	return c.conn.WriteMessage(websocket.BinaryMessage, frame)
}

// readLoop decrypts incoming frames and hands them to the waiting callers.
func (c *Client) readLoop() {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.fail(err)
			return
		}
		resp, err := c.parseFrame(data)
		if err != nil {
			c.fail(err)
			return
		}
		c.dispatch(resp)
	}
}

// parseFrame decrypts one incoming message.
func (c *Client) parseFrame(data []byte) (*response, error) {
	if len(data) < outerHeaderSize {
		return nil, fmt.Errorf("webterm: short frame: %d bytes", len(data))
	}
	// The version field is not checked: the terminal sends 1 and the
	// server answers with 0, and the browser client ignores it too.
	n := binary.LittleEndian.Uint32(data[0:4])
	body := data[outerHeaderSize:]
	if int(n) != len(body) {
		return nil, fmt.Errorf("webterm: frame length mismatch: header %d, actual %d", n, len(body))
	}

	c.mu.Lock()
	cipher := c.cipher
	c.mu.Unlock()

	plain, err := cipher.decrypt(body)
	if err != nil {
		return nil, err
	}
	if len(plain) < responseHeaderSize {
		return nil, fmt.Errorf("webterm: short response: %d bytes", len(plain))
	}
	return &response{
		cmd:  binary.LittleEndian.Uint16(plain[2:4]),
		code: plain[4],
		body: plain[responseHeaderSize:],
	}, nil
}
