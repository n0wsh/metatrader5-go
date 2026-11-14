package webterm

import (
	"context"
	"fmt"
)

// PositionType is the direction of an open position or its initial direction
// in closed-position history. A netting position may reverse during its life.
type PositionType uint32

const (
	PositionTypeBuy PositionType = iota
	PositionTypeSell
)

func (t PositionType) String() string {
	return enumName([]string{"buy", "sell"}, uint32(t))
}

// Position is an open-position snapshot returned by the terminal. PriceCurrent
// and Profit are the values in that snapshot; this client does not subscribe to
// ticks or continuously recalculate them.
type Position struct {
	PositionID     uint64       `json:"position_id"`
	Order          uint64       `json:"order"`
	Time           int64        `json:"time"`        // unix milliseconds
	TimeUpdate     int64        `json:"time_update"` // unix milliseconds
	Symbol         string       `json:"symbol"`
	Type           PositionType `json:"type"`
	Volume         float64      `json:"volume"` // lots
	PriceOpen      float64      `json:"price_open"`
	PriceCurrent   float64      `json:"price_current"`
	SL             float64      `json:"sl"`
	TP             float64      `json:"tp"`
	Profit         float64      `json:"profit"`
	Commission     float64      `json:"commission"`
	Swap           float64      `json:"swap"`
	Expert         uint64       `json:"expert"`
	Comment        string       `json:"comment"`
	ContractSize   float64      `json:"contract_size"`
	Digits         uint32       `json:"digits"`
	DigitsCurrency uint32       `json:"digits_currency"`
	Reason         uint32       `json:"reason"`
	ExternalID     string       `json:"external_id"`
}

// Positions returns the current open positions, including when logged in with
// an investor password. Pending orders in the same response are not included.
func (c *Client) Positions(ctx context.Context) ([]Position, error) {
	resp, err := c.call(ctx, cmdPositions, nil)
	if err != nil {
		return nil, err
	}
	if resp.code != 0 {
		return nil, &StatusError{Command: cmdPositions, Code: resp.code}
	}
	return parsePositions(resp.body)
}

func newPosition(v []any) Position {
	return Position{
		PositionID:     uint64(v[0].(int64)),
		Order:          uint64(v[1].(int64)),
		Time:           dealTime(v[2].(uint32), v[24].(int32)),
		TimeUpdate:     dealTime(v[3].(uint32), v[25].(int32)),
		Symbol:         v[4].(string),
		Type:           PositionType(v[5].(uint32)),
		PriceOpen:      v[6].(float64),
		PriceCurrent:   v[7].(float64),
		SL:             v[8].(float64),
		TP:             v[9].(float64),
		Volume:         float64(v[10].(uint64)) / volumeScale,
		Profit:         v[11].(float64),
		Commission:     v[14].(float64),
		Swap:           v[15].(float64),
		Expert:         uint64(v[16].(int64)),
		Comment:        v[18].(string),
		ContractSize:   v[19].(float64),
		Digits:         v[20].(uint32),
		DigitsCurrency: v[21].(uint32),
		Reason:         v[22].(uint32),
		ExternalID:     v[23].(string),
	}
}

// Command 4 returns a positions section followed by a pending-orders section.
// Validate both counts against the available bytes before allocating slices.
func parsePositions(body []byte) ([]Position, error) {
	positions, _, err := parseOpenState(body)
	return positions, err
}

// OpenOrders returns the pending orders in a fresh command 4 snapshot.
func (c *Client) OpenOrders(ctx context.Context) ([]Order, error) {
	_, orders, err := c.openState(ctx)
	return orders, err
}

func (c *Client) openState(ctx context.Context) ([]Position, []Order, error) {
	resp, err := c.call(ctx, cmdPositions, nil)
	if err != nil {
		return nil, nil, err
	}
	if resp.code != 0 {
		return nil, nil, &StatusError{Command: cmdPositions, Code: resp.code}
	}
	return parseOpenState(resp.body)
}

func parseOpenState(body []byte) ([]Position, []Order, error) {
	n, err := readRecordCount(body, 0, positionRecordSize)
	if err != nil {
		return nil, nil, fmt.Errorf("webterm: positions: %w", err)
	}
	pos := 4
	positions := make([]Position, 0, n)
	for i := 0; i < n; i++ {
		values, err := parse(body, positionSchema, pos)
		if err != nil {
			return nil, nil, err
		}
		positions = append(positions, newPosition(values))
		pos += positionRecordSize
	}
	orderCount, err := readRecordCount(body, pos, orderRecordSize)
	if err != nil {
		return nil, nil, fmt.Errorf("webterm: pending orders: %w", err)
	}
	pos += 4
	orders := make([]Order, 0, orderCount)
	for i := 0; i < orderCount; i++ {
		values, err := parse(body, orderSchema, pos)
		if err != nil {
			return nil, nil, err
		}
		orders = append(orders, newOrder(values))
		pos += orderRecordSize
	}
	// XM build 5660 appends 12 zero bytes per position after both sections.
	// The browser decodes the two counted sections and leaves this suffix
	// unread. Accept only that observed extension, not arbitrary trailing data.
	tail := body[pos:]
	if len(tail) != 0 {
		if n == 0 || len(tail)%12 != 0 || len(tail)/12 != n {
			return nil, nil, fmt.Errorf("webterm: positions reply has %d trailing bytes", len(tail))
		}
		for _, b := range tail {
			if b != 0 {
				return nil, nil, fmt.Errorf("webterm: positions reply has nonzero extension bytes")
			}
		}
	}
	return positions, orders, nil
}
