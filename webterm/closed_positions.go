package webterm

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// ClosedPosition is a completed position lifetime reconstructed from its deals.
// Type is its initial direction; a netting reversal retains the same PositionID.
// Times are Unix milliseconds, using the same broker time convention as Deal.
// Volume is the sum of opening lots, including the new exposure on reversals.
// PriceOpen and PriceClose are volume-weighted entry and exit execution prices;
// for reversed positions these include executions in both directions. Profit
// comes from the terminal, rather than being inferred from these prices.
// Commission, Swap and Fee include the values on all related trading deals.
// Separately booked balance or commission operations are not included.
// Service re-openings or corrections that reuse a fully closed position's ID
// cannot be reconstructed by this helper and return PositionHistoryError.
type ClosedPosition struct {
	PositionID uint64       `json:"position_id"`
	Symbol     string       `json:"symbol"`
	Type       PositionType `json:"type"`
	TimeOpen   int64        `json:"time_open"`
	TimeClose  int64        `json:"time_close"`
	Volume     float64      `json:"volume"`
	PriceOpen  float64      `json:"price_open"`
	PriceClose float64      `json:"price_close"`
	Profit     float64      `json:"profit"`
	Commission float64      `json:"commission"`
	Swap       float64      `json:"swap"`
	Fee        float64      `json:"fee"`
	NetProfit  float64      `json:"net_profit"`
	Deals      []Deal       `json:"deals"`
}

// PositionHistoryError identifies incomplete or inconsistent deal history. A
// closing deal without a known opener cannot establish a completed position.
// The server may have archived the missing deals; requesting an earlier start
// only helps when those records are still available.
type PositionHistoryError struct {
	PositionID uint64
	DealTicket uint64
	Reason     string
}

func (e *PositionHistoryError) Error() string {
	return fmt.Sprintf("webterm: position %d, deal %d: %s (incomplete or inconsistent position history)", e.PositionID, e.DealTicket, e.Reason)
}

// HistoryPositions returns positions fully closed during the inclusive interval,
// ordered by close time and PositionID. A zero from has no lower limit and zero
// to means now. Opening deals can precede from, so this method requests all
// available history through the second after to, then filters by the exact
// final closing time. A nonzero to must have Unix seconds in [1, 2147483646]
// because the wire uses signed 32-bit seconds and zero means now. The fetch
// can be substantially larger than HistoryDeals(ctx, from, to).
//
// Partial closes and netting reversals are retained in the same lifetime until
// its remaining volume is zero. Missing openers or inconsistent transitions
// return *PositionHistoryError instead of an inaccurate completed position.
// See History for broker-time considerations.
func (c *Client) HistoryPositions(ctx context.Context, from, to time.Time) ([]ClosedPosition, error) {
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, fmt.Errorf("webterm: position history start is after end")
	}
	queryTo := to
	if !to.IsZero() {
		sec := to.Unix()
		if sec <= 0 || sec >= math.MaxInt32 {
			return nil, fmt.Errorf("webterm: position history end must have Unix seconds in [1, %d]", math.MaxInt32-1)
		}
		// Include the final second regardless of whether the server treats
		// its seconds-only upper bound as inclusive or exclusive.
		queryTo = time.Unix(sec+1, 0)
	}
	deals, err := c.HistoryDeals(ctx, time.Time{}, queryTo)
	if err != nil {
		return nil, err
	}
	// Enforce the millisecond bound even if the terminal responds with the whole
	// final second or a wider range than requested.
	if !to.IsZero() {
		end := to.UnixMilli()
		filtered := make([]Deal, 0, len(deals))
		for _, d := range deals {
			if d.Time <= end {
				filtered = append(filtered, d)
			}
		}
		deals = filtered
	}
	positions, err := ClosedPositions(deals)
	if err != nil {
		return nil, err
	}
	out := make([]ClosedPosition, 0, len(positions))
	for _, p := range positions {
		if !from.IsZero() && time.UnixMilli(p.TimeClose).Before(from) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// ClosedPositions reconstructs fully closed positions from a complete set of
// trading deals. It copies and sorts the input by Time and Ticket, groups by
// PositionID, and does not modify the caller's slice. Positions that remain open
// are omitted. Balance operations and other non-buy/sell deals are ignored.
//
// This cannot detect every missing record (for example, a missing scale-in and
// matching scale-out). Supply the complete history for each included position.
func ClosedPositions(deals []Deal) ([]ClosedPosition, error) {
	ordered := append([]Deal(nil), deals...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Time != ordered[j].Time {
			return ordered[i].Time < ordered[j].Time
		}
		return ordered[i].Ticket < ordered[j].Ticket
	})
	type lifetime struct {
		position   ClosedPosition
		remaining  int64
		opened     int64
		closed     int64
		direction  DealType
		entryValue float64
		exitValue  float64
	}
	states := make(map[uint64]*lifetime)
	tickets := make(map[uint64]bool)
	for _, d := range ordered {
		if d.Type != DealTypeBuy && d.Type != DealTypeSell {
			continue
		}
		historyError := func(reason string) error {
			return &PositionHistoryError{PositionID: d.PositionID, DealTicket: d.Ticket, Reason: reason}
		}
		if d.PositionID == 0 {
			return nil, historyError("trading deal has no position identifier")
		}
		if tickets[d.Ticket] {
			return nil, historyError("duplicate trading deal ticket")
		}
		tickets[d.Ticket] = true
		scaled := d.Volume * volumeScale
		if math.IsNaN(scaled) || math.IsInf(scaled, 0) || scaled <= 0 || scaled >= 1<<53 || math.Abs(scaled-math.Round(scaled)) > 0.0001 {
			return nil, historyError("volume must be positive lots in increments of 1e-8")
		}
		units := int64(math.Round(scaled))
		if units == 0 {
			return nil, historyError("trading volume rounds to zero")
		}
		if math.IsNaN(d.PriceOpen) || math.IsInf(d.PriceOpen, 0) || math.IsNaN(d.PriceClose) || math.IsInf(d.PriceClose, 0) {
			return nil, historyError("non-finite execution price")
		}
		s := states[d.PositionID]
		if s == nil {
			if d.Entry != DealEntryIn {
				return nil, historyError("opening deal is missing")
			}
			s = &lifetime{position: ClosedPosition{PositionID: d.PositionID, Symbol: d.Symbol, Type: PositionType(d.Type), TimeOpen: d.Time}}
			s.direction = d.Type
			states[d.PositionID] = s
		} else {
			if s.position.Symbol != d.Symbol {
				return nil, historyError("symbol changed within a position")
			}
			if s.remaining == 0 {
				return nil, historyError("position identifier reused after final closure")
			}
		}
		var opening, closing int64
		switch d.Entry {
		case DealEntryIn:
			if d.Type != s.direction {
				return nil, historyError("entry direction opposes the current position")
			}
			opening = units
		case DealEntryOut, DealEntryOutBy:
			if d.Type == s.direction {
				return nil, historyError("exit direction matches the current position")
			}
			if units > s.remaining {
				return nil, historyError("exit volume exceeds known open volume")
			}
			closing = units
		case DealEntryInOut:
			if d.Type == s.direction || units <= s.remaining {
				return nil, historyError("reversal must exceed opposite open volume")
			}
			closing, opening = s.remaining, units-s.remaining
			s.direction = d.Type
		default:
			return nil, historyError("unknown trading deal entry")
		}
		if opening > math.MaxInt64-s.opened {
			return nil, historyError("accumulated volume overflows")
		}
		s.remaining += opening - closing
		s.opened += opening
		s.closed += closing
		s.entryValue += float64(opening) * d.PriceOpen
		// PriceOpen carries the deal execution price in the web-terminal record.
		s.exitValue += float64(closing) * d.PriceOpen
		s.position.Profit += d.Profit
		s.position.Commission += d.Commission
		s.position.Swap += d.Swap
		s.position.Fee += d.Fee
		s.position.Deals = append(s.position.Deals, d)
		if s.remaining == 0 {
			s.position.TimeClose = d.Time
		}
	}
	out := make([]ClosedPosition, 0, len(states))
	for _, s := range states {
		if s.remaining != 0 {
			continue
		}
		p := s.position
		p.Volume = float64(s.opened) / volumeScale
		p.PriceOpen = s.entryValue / float64(s.opened)
		p.PriceClose = s.exitValue / float64(s.closed)
		p.NetProfit = p.Profit + p.Commission + p.Swap + p.Fee
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TimeClose != out[j].TimeClose {
			return out[i].TimeClose < out[j].TimeClose
		}
		return out[i].PositionID < out[j].PositionID
	})
	return out, nil
}
