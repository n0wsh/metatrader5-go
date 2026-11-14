package mt5legacy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// DefaultLoginTimeout matches the Python package's default wait for the
// terminal to authorize after a login request.
const DefaultLoginTimeout = 60 * time.Second

const authPollInterval = 100 * time.Millisecond

// deadliner is implemented by net.Conn and go-winio pipes.
type deadliner interface {
	SetDeadline(time.Time) error
}

// Client is a connection to one MetaTrader 5 terminal. A terminal is logged
// into one account at a time, so Login switches the account for every client
// of that terminal. Methods are safe for concurrent use but run one at a time.
type Client struct {
	mu    sync.Mutex
	conn  io.ReadWriteCloser
	build uint32
}

// NewClient performs the API handshake over an already-open terminal
// connection (see DialPipe, DialTerminal and DialTCP).
func NewClient(ctx context.Context, conn io.ReadWriteCloser) (*Client, error) {
	c := &Client{conn: conn}
	req := newRequest(cmdHello)
	req.u32(helloVersion)
	req.wstr(helloClient)
	body, err := c.call(ctx, cmdHello, req)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	c.build = body.u32()
	if body.err != nil {
		_ = conn.Close()
		return nil, body.err
	}
	return c, nil
}

// TerminalBuild is the terminal build number reported during the handshake.
func (c *Client) TerminalBuild() uint32 { return c.build }

// Close closes the connection. The terminal keeps running.
func (c *Client) Close() error { return c.conn.Close() }

// Login switches the terminal to the given account and waits until it is
// authorized. An investor (read-only) password gives full access to history
// and account state, and any trade request fails.
func (c *Client) Login(ctx context.Context, login uint64, password, server string) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultLoginTimeout)
		defer cancel()
	}
	req := newRequest(cmdLogin)
	req.u32(0)
	req.u64(login)
	req.wstr(password)
	req.wstr(server)
	if _, err := c.call(ctx, cmdLogin, req); err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == ResUnsupported {
			return ErrUnsupportedAuth
		}
		return err
	}
	err := c.waitAuthorized(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return resError(ResInternalFailTimeout)
	}
	return err
}

func (c *Client) waitAuthorized(ctx context.Context) error {
	t := time.NewTicker(authPollInterval)
	defer t.Stop()
	for {
		body, err := c.call(ctx, cmdAuthState, newRequest(cmdAuthState))
		if err != nil {
			return err
		}
		switch state := body.u32(); {
		case body.err != nil:
			return body.err
		case state == authStateAuthorized:
			return nil
		case state == authStateFailed:
			return ErrAuthFailed
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// HistoryDeals returns deals in [from, to]. MT5 compares these against broker
// server time, so pass the server wall clock as if it were UTC. group is an
// optional symbol filter such as "*USD*,!EUR*"; empty means all symbols.
func (c *Client) HistoryDeals(ctx context.Context, from, to time.Time, group string) ([]Deal, error) {
	req := newRequest(cmdHistoryDealsByRange)
	req.i64(from.Unix())
	req.i64(to.Unix())
	deals, err := callList(c, ctx, cmdHistoryDealsByRange, req, dealRecordSize, decodeDeal)
	if err != nil || group == "" {
		return deals, err
	}
	out := deals[:0]
	for _, d := range deals {
		if matchGroup(group, d.Symbol) {
			out = append(out, d)
		}
	}
	return out, nil
}

// HistoryDealsByPosition returns all deals of a position.
func (c *Client) HistoryDealsByPosition(ctx context.Context, positionID uint64) ([]Deal, error) {
	req := newRequest(cmdHistoryDealsByPosition)
	req.u64(positionID)
	return callList(c, ctx, cmdHistoryDealsByPosition, req, dealRecordSize, decodeDeal)
}

// HistoryDealByTicket returns a single deal.
func (c *Client) HistoryDealByTicket(ctx context.Context, ticket uint64) (Deal, error) {
	req := newRequest(cmdHistoryDealsByTicket)
	req.u64(ticket)
	return callOne(c, ctx, cmdHistoryDealsByTicket, req, decodeDeal)
}

// HistoryOrders returns historical orders in [from, to]; see HistoryDeals for
// the time and group semantics.
func (c *Client) HistoryOrders(ctx context.Context, from, to time.Time, group string) ([]Order, error) {
	req := newRequest(cmdHistoryOrdersByRange)
	req.i64(from.Unix())
	req.i64(to.Unix())
	orders, err := callList(c, ctx, cmdHistoryOrdersByRange, req, orderRecordSize, decodeOrder)
	if err != nil || group == "" {
		return orders, err
	}
	out := orders[:0]
	for _, o := range orders {
		if matchGroup(group, o.Symbol) {
			out = append(out, o)
		}
	}
	return out, nil
}

// HistoryOrdersByPosition returns all historical orders of a position.
func (c *Client) HistoryOrdersByPosition(ctx context.Context, positionID uint64) ([]Order, error) {
	req := newRequest(cmdHistoryOrdersByPosition)
	req.u64(positionID)
	return callList(c, ctx, cmdHistoryOrdersByPosition, req, orderRecordSize, decodeOrder)
}

// HistoryOrderByTicket returns a single historical order.
func (c *Client) HistoryOrderByTicket(ctx context.Context, ticket uint64) (Order, error) {
	req := newRequest(cmdHistoryOrdersByTicket)
	req.u64(ticket)
	return callOne(c, ctx, cmdHistoryOrdersByTicket, req, decodeOrder)
}

func callList[T any](c *Client, ctx context.Context, cmd uint32, req *encoder, size int, decode func(*decoder) T) ([]T, error) {
	body, err := c.call(ctx, cmd, req)
	if err != nil {
		return nil, err
	}
	n := body.u32()
	if body.err == nil && len(body.b) != int(n)*size {
		return nil, fmt.Errorf("mt5: cmd %#x: %d records but %d bytes", cmd, n, len(body.b))
	}
	out := make([]T, 0, n)
	for i := uint32(0); i < n && body.err == nil; i++ {
		out = append(out, decode(body))
	}
	return out, body.err
}

func callOne[T any](c *Client, ctx context.Context, cmd uint32, req *encoder, decode func(*decoder) T) (T, error) {
	var zero T
	body, err := c.call(ctx, cmd, req)
	if err != nil {
		return zero, err
	}
	v := decode(body)
	if body.err != nil {
		return zero, body.err
	}
	return v, nil
}

// call sends one request and reads its response, mapping negative retcodes
// to *Error.
func (c *Client) call(ctx context.Context, cmd uint32, req *encoder) (*decoder, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if dl, ok := c.conn.(deadliner); ok {
		deadline, _ := ctx.Deadline()
		if err := dl.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("mt5: set deadline: %w", err)
		}
		stop := context.AfterFunc(ctx, func() {
			if err := dl.SetDeadline(time.Unix(1, 0)); err != nil {
				// Closing the connection also interrupts I/O if its deadline fails.
				_ = c.conn.Close()
			}
		})
		defer stop()
	}
	if err := writeRequest(c.conn, req); err != nil {
		return nil, c.ioErr(ctx, ResInternalFailSend, err)
	}
	rcmd, ret, body, err := readResponse(c.conn)
	if err != nil {
		return nil, c.ioErr(ctx, ResInternalFailReceive, err)
	}
	if ret < 0 {
		return nil, resError(ret)
	}
	if rcmd != cmd {
		return nil, fmt.Errorf("mt5: response cmd %#x, want %#x", rcmd, cmd)
	}
	return body, nil
}

// ioErr closes the connection, since a partial exchange leaves the stream out
// of sync.
func (c *Client) ioErr(ctx context.Context, code int32, err error) error {
	_ = c.conn.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A socket deadline can fire before the context timer publishes its error.
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	e := resError(code)
	return fmt.Errorf("%w: %v", e, err)
}
