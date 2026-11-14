package webterm

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestSLTPHistoryPreservesEvidence(t *testing.T) {
	h := &History{
		Deals: []Deal{
			{Ticket: 3, Order: 20, PositionID: 10, Type: DealTypeSell, Time: 300, SL: 1.1, TP: 1.3},
			{Ticket: 1, Order: 20, PositionID: 10, Type: DealTypeBuy, Time: 100},
			{Ticket: 2, Type: DealTypeBalance, Time: 50},
		},
		Orders: []Order{
			{Ticket: 20, PositionID: 10, TimeSetup: 90, TimeDone: 100},
			{Ticket: 21, TimeSetup: 200, SL: 1.2, TP: 1.4},
		},
	}
	before := append([]Deal(nil), h.Deals...)
	got := SLTPHistory(h)
	if len(got) != 4 || got[0].DealTicket != 1 || got[1].OrderTicket != 20 || got[2].OrderTicket != 21 || got[3].DealTicket != 3 {
		t.Fatalf("evidence order: %+v", got)
	}
	if got[0].SL != 0 || got[0].TP != 0 || got[1].RecordTime != 100 || got[1].RecordTimeKind != "order_done" || got[2].RecordTimeKind != "order_setup" || got[2].PositionID != 0 {
		t.Fatalf("lost zero levels, timestamps, or unlinked order: %+v", got)
	}
	for _, obs := range got {
		if obs.Kind != "history" || obs.Previous != nil || obs.ObservedAt != 0 || obs.PreviousObservedAt != 0 {
			t.Fatalf("invented modification: %+v", obs)
		}
	}
	if !reflect.DeepEqual(before, h.Deals) {
		t.Fatal("input mutated")
	}
	if got := SLTPHistory(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty history = %v", got)
	}
}

func TestSLTPTrackerChangesAndGaps(t *testing.T) {
	var tracker SLTPTracker
	base := time.Unix(1700000000, 0)
	positions := []Position{{PositionID: 42, Symbol: "EURUSD", SL: 1.1, TP: 1.3}}
	orders := []Order{{Ticket: 42, Symbol: "EURUSD", SL: 1, TP: 1.4}}
	observe := func(step int) []SLTPObservation {
		t.Helper()
		got, err := tracker.Observe(base.Add(time.Duration(step)*time.Second), positions, orders)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := observe(0)
	if len(first) != 2 || first[0].Kind != "baseline" || first[1].Previous != nil {
		t.Fatalf("baselines: %+v", first)
	}
	positions[0].PriceCurrent = 1.2
	positions[0].TimeUpdate = 123
	if got := observe(1); len(got) != 0 {
		t.Fatalf("non-SL/TP change emitted: %+v", got)
	}
	positions[0].SL = 0 // Removing a stop must not be treated as missing data.
	orders[0].TP = 0
	changes := observe(2)
	if len(changes) != 2 {
		t.Fatalf("changes: %+v", changes)
	}
	for _, obs := range changes {
		if obs.Kind != "change" || obs.Previous == nil || obs.PreviousObservedAt != base.Add(time.Second).UnixMilli() {
			t.Fatalf("incorrect change bounds: %+v", obs)
		}
	}
	if changes[0].TP != 0 || changes[0].Previous.TP != 1.4 || changes[1].SL != 0 || changes[1].Previous.SL != 1.1 {
		t.Fatalf("removed levels: %+v", changes)
	}
	// Equal local IDs for a position and an order must stay independent.
	positions, orders = nil, nil
	if got := observe(3); len(got) != 0 {
		t.Fatalf("disappearance inferred a modification: %+v", got)
	}
	positions = []Position{{PositionID: 42, SL: 1.15}}
	if got := observe(4); len(got) != 1 || got[0].Kind != "baseline" || got[0].Previous != nil {
		t.Fatalf("reappearance: %+v", got)
	}
	tracker.Reset()
	if got := observe(0); len(got) != 1 || got[0].Kind != "baseline" {
		t.Fatalf("reset: %+v", got)
	}
	if first[1].SL != 1.1 || first[1].Previous != nil {
		t.Fatal("previously returned event mutated")
	}
}

func TestSLTPTrackerRejectsInvalidSnapshotsAtomically(t *testing.T) {
	base := time.Unix(1700000000, 0)
	for _, bad := range []struct {
		name      string
		at        time.Time
		positions []Position
	}{
		{"zero time", time.Time{}, []Position{{PositionID: 1}}},
		{"old time", base, []Position{{PositionID: 1}}},
		{"zero ID", base.Add(time.Second), []Position{{}}},
		{"duplicate", base.Add(time.Second), []Position{{PositionID: 1}, {PositionID: 1}}},
		{"nan", base.Add(time.Second), []Position{{PositionID: 1, SL: math.NaN()}}},
		{"infinite", base.Add(time.Second), []Position{{PositionID: 1, TP: math.Inf(1)}}},
	} {
		t.Run(bad.name, func(t *testing.T) {
			var tracker SLTPTracker
			if _, err := tracker.Observe(base, []Position{{PositionID: 1, SL: 10}}, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := tracker.Observe(bad.at, bad.positions, nil); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			got, err := tracker.Observe(base.Add(2*time.Second), []Position{{PositionID: 1, SL: 20}}, nil)
			if err != nil || len(got) != 1 || got[0].Previous == nil || got[0].Previous.SL != 10 {
				t.Fatalf("baseline lost after rejected snapshot: %+v, %v", got, err)
			}
		})
	}
}

func sltpWireSnapshot(t *testing.T) []byte {
	t.Helper()
	body := append([]byte(nil), positionFixture(t)...)
	// The synthetic pending order is otherwise zero-filled.
	binary.LittleEndian.PutUint64(body[4+344+4:], 777)
	return body
}

func TestWatchSLTPWireChangesAndHeartbeat(t *testing.T) {
	body := sltpWireSnapshot(t)
	changed := append([]byte(nil), body...)
	binary.LittleEndian.PutUint64(changed[4+108:], math.Float64bits(0))
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		for _, snapshot := range [][]byte{body, body, changed} {
			cmd, payload, err := p.read()
			if err != nil {
				t.Error(err)
				return
			}
			if cmd != cmdPositions || len(payload) != 0 {
				t.Errorf("wrong watch request: %d %x", cmd, payload)
				return
			}
			if err := p.reply(cmd, 0, snapshot); err != nil {
				t.Error(err)
				return
			}
		}
		p.read()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var batches [][]SLTPObservation
	err := c.WatchSLTP(ctx, time.Millisecond, func(at time.Time, obs []SLTPObservation) error {
		if at.IsZero() {
			t.Error("missing heartbeat timestamp")
		}
		batches = append(batches, obs)
		if len(batches) == 3 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || len(batches) != 3 || len(batches[0]) != 2 || len(batches[1]) != 0 || len(batches[2]) != 1 {
		t.Fatalf("watch result: %+v, %v", batches, err)
	}
	last := batches[2][0]
	if last.Kind != "change" || last.SL != 0 || last.Previous == nil || last.Previous.SL != 1.15 {
		t.Fatalf("wire change lost: %+v", last)
	}
}

func TestWatchSLTPStopsOnErrors(t *testing.T) {
	storageErr := errors.New("storage unavailable")
	for _, tc := range []struct {
		name       string
		body       []byte
		code       uint8
		handlerErr error
	}{
		{"storage", make([]byte, 8), 0, storageErr},
		{"parse", make([]byte, 20), 0, nil},
		{"status", nil, 3, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
				cmd, _, err := p.read()
				if err != nil {
					t.Error(err)
					return
				}
				if err := p.reply(cmd, tc.code, tc.body); err != nil {
					t.Error(err)
					return
				}
				p.read()
			})
			calls := 0
			err := c.WatchSLTP(context.Background(), time.Second, func(time.Time, []SLTPObservation) error { calls++; return tc.handlerErr })
			if err == nil {
				t.Fatal("watch swallowed failure")
			}
			if tc.handlerErr != nil {
				if !errors.Is(err, storageErr) || calls != 1 {
					t.Fatalf("storage failure: %v, calls=%d", err, calls)
				}
			} else if calls != 0 {
				t.Fatal("handler saw an invalid snapshot")
			}
		})
	}
	var c Client
	if err := c.WatchSLTP(context.Background(), 0, func(time.Time, []SLTPObservation) error { return nil }); err == nil {
		t.Fatal("zero interval accepted")
	}
	if err := c.WatchSLTP(context.Background(), time.Second, nil); err == nil {
		t.Fatal("nil handler accepted")
	}
}

func TestOpenOrdersWire(t *testing.T) {
	body := sltpWireSnapshot(t)
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		cmd, _, err := p.read()
		if err != nil {
			t.Error(err)
			return
		}
		if err := p.reply(cmd, 0, body); err != nil {
			t.Error(err)
			return
		}
		p.read()
	})
	orders, err := c.OpenOrders(context.Background())
	if err != nil || len(orders) != 1 || orders[0].Ticket != 777 {
		t.Fatalf("pending orders: %+v, %v", orders, err)
	}
}

func TestHistorySLTPWire(t *testing.T) {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, 1)
	body = append(body, buildDeal(t, 123, "EURUSD", "history", 0.1)...)
	body = append(body, 0, 0, 0, 0)
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		cmd, payload, err := p.read()
		if err != nil {
			t.Error(err)
			return
		}
		if cmd != cmdTradeHistory || len(payload) != 8 {
			t.Errorf("history request %d %x", cmd, payload)
			return
		}
		if err := p.reply(cmd, 0, body); err != nil {
			t.Error(err)
			return
		}
		p.read()
	})
	got, err := c.HistorySLTP(context.Background(), time.Time{}, time.Time{})
	if err != nil || len(got) != 1 || got[0].DealTicket != 123 || got[0].SL != 0 || got[0].TP != 1.2 || got[0].ObservedAt == 0 {
		t.Fatalf("historical wire levels: %+v, %v", got, err)
	}
}
