package webterm

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// SLTPLevels contains the stop-loss and take-profit levels returned by the
// terminal. Zero values are retained, including changes that remove a level.
type SLTPLevels struct {
	SL float64 `json:"sl"`
	TP float64 `json:"tp"`
}

// SLTPObservation is evidence of SL/TP values, not necessarily a modification.
// Source is deal, order, position, or pending_order. Kind is history, baseline,
// or change. History entries retain separate deal/order records even when the
// levels match. RecordTime is broker time in Unix milliseconds; RecordTimeKind
// describes that timestamp, not the time the SL/TP was last modified.
// ObservedAt and PreviousObservedAt are local UTC Unix milliseconds. A change
// means levels differ between those observations; intermediate changes may
// have been missed. Previous is absent for history entries and live baselines.
type SLTPObservation struct {
	Source             string      `json:"source"`
	Kind               string      `json:"kind"`
	PositionID         uint64      `json:"position_id"`
	OrderTicket        uint64      `json:"order_ticket,omitempty"`
	DealTicket         uint64      `json:"deal_ticket,omitempty"`
	Symbol             string      `json:"symbol"`
	SL                 float64     `json:"sl"`
	TP                 float64     `json:"tp"`
	RecordTime         int64       `json:"record_time,omitempty"`
	RecordTimeKind     string      `json:"record_time_kind,omitempty"`
	ObservedAt         int64       `json:"observed_at,omitempty"`
	Previous           *SLTPLevels `json:"previous,omitempty"`
	PreviousObservedAt int64       `json:"previous_observed_at,omitempty"`
}

// SLTPHistory extracts available SL/TP evidence from loaded history, ordered by
// record time, source, and ticket. Nontrading deals are excluded. Orders without
// a PositionID (for example canceled pending orders) are retained by OrderTicket.
// Order levels describe the returned order record, not necessarily its initial
// setup values. No intermediate modifications or previous levels are inferred.
// Input records are neither mutated nor deduplicated.
func SLTPHistory(h *History) []SLTPObservation {
	out := make([]SLTPObservation, 0)
	if h == nil {
		return out
	}
	for _, d := range h.Deals {
		if d.Type != DealTypeBuy && d.Type != DealTypeSell {
			continue
		}
		out = append(out, SLTPObservation{
			Source: "deal", Kind: "history", PositionID: d.PositionID,
			OrderTicket: d.Order, DealTicket: d.Ticket, Symbol: d.Symbol,
			SL: d.SL, TP: d.TP, RecordTime: d.Time, RecordTimeKind: "deal_execution",
		})
	}
	for _, o := range h.Orders {
		stamp, kind := o.TimeDone, "order_done"
		if stamp == 0 {
			stamp, kind = o.TimeSetup, "order_setup"
		}
		out = append(out, SLTPObservation{
			Source: "order", Kind: "history", PositionID: o.PositionID,
			OrderTicket: o.Ticket, Symbol: o.Symbol, SL: o.SL, TP: o.TP,
			RecordTime: stamp, RecordTimeKind: kind,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.RecordTime != b.RecordTime {
			return a.RecordTime < b.RecordTime
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.DealTicket != b.DealTicket {
			return a.DealTicket < b.DealTicket
		}
		return a.OrderTicket < b.OrderTicket
	})
	return out
}

// HistorySLTP fetches history and extracts its available SL/TP observations.
// It uses the same time window and broker-time convention as History.
func (c *Client) HistorySLTP(ctx context.Context, from, to time.Time) ([]SLTPObservation, error) {
	h, err := c.History(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := SLTPHistory(h)
	at := time.Now().UnixMilli()
	for i := range out {
		out[i].ObservedAt = at
	}
	return out, nil
}

type sltpKey struct {
	source string
	id     uint64
}

type sltpSample struct {
	levels SLTPLevels
	at     int64
}

// SLTPTracker compares successive complete snapshots of one account. Its zero
// value is ready to use. It emits a baseline for each newly seen position/order
// and changes thereafter. Missing entities are forgotten, not declared closed.
// Reappearing entities start a new baseline. Reset after a connection gap, and
// use separate trackers for different accounts. Methods are concurrency safe;
// callers must still supply snapshots in increasing observation-time order.
type SLTPTracker struct {
	mu   sync.Mutex
	last time.Time
	seen map[sltpKey]sltpSample
}

// Reset discards baselines. It does not try to bridge a period without coverage.
func (t *SLTPTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last = time.Time{}
	t.seen = nil
}

// Observe compares a complete snapshot with the preceding successful snapshot.
// ObservedAt must be nonzero and later than the preceding observation. Invalid
// snapshots return an error without changing tracker state. Position.TimeUpdate
// is retained as record metadata; it is not assumed to be an SL/TP-change time.
func (t *SLTPTracker) Observe(at time.Time, positions []Position, orders []Order) ([]SLTPObservation, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if at.IsZero() || (!t.last.IsZero() && !at.After(t.last)) {
		return nil, fmt.Errorf("webterm: SL/TP observation time must increase and be nonzero")
	}
	next := make(map[sltpKey]sltpSample, len(positions)+len(orders))
	out := make([]SLTPObservation, 0)
	observe := func(key sltpKey, obs SLTPObservation) error {
		if key.id == 0 || math.IsNaN(obs.SL) || math.IsNaN(obs.TP) || math.IsInf(obs.SL, 0) || math.IsInf(obs.TP, 0) {
			return fmt.Errorf("webterm: invalid SL/TP snapshot for %s %d", key.source, key.id)
		}
		if _, exists := next[key]; exists {
			return fmt.Errorf("webterm: duplicate SL/TP snapshot for %s %d", key.source, key.id)
		}
		levels := SLTPLevels{SL: obs.SL, TP: obs.TP}
		next[key] = sltpSample{levels: levels, at: at.UnixMilli()}
		obs.ObservedAt = at.UnixMilli()
		previous, exists := t.seen[key]
		if !exists {
			obs.Kind = "baseline"
		} else {
			if previous.levels == levels {
				return nil
			}
			obs.Kind = "change"
			old := previous.levels
			obs.Previous = &old
			obs.PreviousObservedAt = previous.at
		}
		out = append(out, obs)
		return nil
	}
	for _, p := range positions {
		if err := observe(sltpKey{"position", p.PositionID}, SLTPObservation{
			Source: "position", PositionID: p.PositionID, OrderTicket: p.Order,
			Symbol: p.Symbol, SL: p.SL, TP: p.TP,
			RecordTime: p.TimeUpdate, RecordTimeKind: "position_update",
		}); err != nil {
			return nil, err
		}
	}
	for _, o := range orders {
		if err := observe(sltpKey{"pending_order", o.Ticket}, SLTPObservation{
			Source: "pending_order", PositionID: o.PositionID, OrderTicket: o.Ticket,
			Symbol: o.Symbol, SL: o.SL, TP: o.TP,
			RecordTime: o.TimeSetup, RecordTimeKind: "order_setup",
		}); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].PositionID != out[j].PositionID {
			return out[i].PositionID < out[j].PositionID
		}
		return out[i].OrderTicket < out[j].OrderTicket
	})
	t.seen, t.last = next, at
	return out, nil
}

// WatchSLTP polls positions and pending orders immediately and then waits interval
// after each successful snapshot and handler call. It calls handle once per
// snapshot, including when observations is empty (a coverage heartbeat). Handle
// runs synchronously: persist observations there and return any storage error.
// Cancellation, snapshot/parse failures, and handler errors stop the watch and
// are returned. Restarting creates new baselines rather than bridging the gap.
// Polling cannot capture changes between samples or recover past modifications.
// Login before calling; do not change accounts or log out during the watch.
func (c *Client) WatchSLTP(ctx context.Context, interval time.Duration, handle func(observedAt time.Time, observations []SLTPObservation) error) error {
	if interval <= 0 || handle == nil {
		return fmt.Errorf("webterm: SL/TP watch requires a positive interval and a handler")
	}
	var tracker SLTPTracker
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		positions, orders, err := c.openState(ctx)
		if err != nil {
			return err
		}
		at := time.Now()
		observations, err := tracker.Observe(at, positions, orders)
		if err != nil {
			return err
		}
		if err := handle(at, observations); err != nil {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
