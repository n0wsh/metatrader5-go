package webterm

import (
	"context"
	"encoding/binary"
	"time"
)

// History returns the deals and orders of the given period. The times are
// Unix seconds; a zero time means "no limit" for from and "now" for to,
// which is what the terminal itself sends.
func (c *Client) History(ctx context.Context, from, to time.Time) (*History, error) {
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload[0:], uint32(int32(unixSeconds(from))))
	binary.LittleEndian.PutUint32(payload[4:], uint32(int32(unixSeconds(to))))
	resp, err := c.call(ctx, cmdTradeHistory, payload)
	if err != nil {
		return nil, err
	}
	if resp.code != 0 {
		return nil, &StatusError{Command: cmdTradeHistory, Code: resp.code}
	}
	return parseHistory(resp.body)
}

// unixSeconds returns the Unix seconds of t, or zero for the zero time,
// which the terminal reads as "everything" or "now".
func unixSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// HistoryDeals returns only the deals of the given period.
func (c *Client) HistoryDeals(ctx context.Context, from, to time.Time) ([]Deal, error) {
	h, err := c.History(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return h.Deals, nil
}

// HistoryOrders returns only the orders of the given period.
func (c *Client) HistoryOrders(ctx context.Context, from, to time.Time) ([]Order, error) {
	h, err := c.History(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return h.Orders, nil
}
