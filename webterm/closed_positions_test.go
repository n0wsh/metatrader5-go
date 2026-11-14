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

func historyTrade(ticket, position uint64, at int64, typ DealType, entry DealEntry, volume, price float64) Deal {
	return Deal{Ticket: ticket, PositionID: position, Time: at, Symbol: "EURUSD", Type: typ, Entry: entry, Volume: volume, PriceOpen: price}
}

func TestClosedPositionsPartialClosesAndScaleIn(t *testing.T) {
	deals := []Deal{
		historyTrade(1, 50, 10, DealTypeBuy, DealEntryIn, 0.1, 100),
		historyTrade(2, 50, 20, DealTypeBuy, DealEntryIn, 0.2, 103),
		historyTrade(3, 50, 30, DealTypeSell, DealEntryOut, 0.1, 105),
		historyTrade(4, 50, 40, DealTypeSell, DealEntryOut, 0.2, 108),
	}
	deals[0].Commission = -1
	deals[1].Fee = -0.5
	deals[2].Profit, deals[2].Commission = 3, -0.25
	deals[3].Profit, deals[3].Swap = 7, -0.5
	got, err := ClosedPositions(deals)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("positions = %v", got)
	}
	p := got[0]
	if p.PositionID != 50 || p.Symbol != "EURUSD" || p.Type != PositionTypeBuy || p.TimeOpen != 10 || p.TimeClose != 40 {
		t.Fatalf("identity = %+v", p)
	}
	if math.Abs(p.Volume-0.3) > 1e-12 || math.Abs(p.PriceOpen-102) > 1e-12 || math.Abs(p.PriceClose-107) > 1e-12 {
		t.Fatalf("weighted values = %+v", p)
	}
	if p.Profit != 10 || p.Commission != -1.25 || p.Swap != -0.5 || p.Fee != -0.5 || p.NetProfit != 7.75 {
		t.Fatalf("profit/costs = %+v", p)
	}
	if !reflect.DeepEqual(p.Deals, deals) {
		t.Fatalf("deals = %v", p.Deals)
	}
	got, err = ClosedPositions(deals[:3])
	if err != nil || len(got) != 0 {
		t.Fatalf("partially closed position reported as complete: %v, %v", got, err)
	}
}

func TestClosedPositionsReversalRemainsSameLifetime(t *testing.T) {
	deals := []Deal{
		historyTrade(1, 40, 10, DealTypeBuy, DealEntryIn, 1, 100),
		historyTrade(2, 40, 20, DealTypeSell, DealEntryInOut, 3, 110),
		historyTrade(3, 40, 30, DealTypeBuy, DealEntryOut, 2, 105),
	}
	deals[1].Profit, deals[2].Profit = 10, 10
	got, err := ClosedPositions(deals[:2])
	if err != nil || len(got) != 0 {
		t.Fatalf("reversal closed the position: %v, %v", got, err)
	}
	got, err = ClosedPositions(deals)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("positions = %v", got)
	}
	p := got[0]
	if p.Type != PositionTypeBuy || p.TimeOpen != 10 || p.TimeClose != 30 || p.Volume != 3 || p.Profit != 20 || len(p.Deals) != 3 {
		t.Fatalf("lifetime = %+v", p)
	}
	if math.Abs(p.PriceOpen-(100+2*110)/3.0) > 1e-12 || math.Abs(p.PriceClose-(110+2*105)/3.0) > 1e-12 {
		t.Fatalf("reversal prices = %+v", p)
	}
}

func TestClosedPositionsHedgingCloseBy(t *testing.T) {
	deals := []Deal{
		historyTrade(1, 10, 10, DealTypeBuy, DealEntryIn, 1, 100),
		historyTrade(2, 20, 20, DealTypeSell, DealEntryIn, 1, 103),
		historyTrade(3, 10, 30, DealTypeSell, DealEntryOutBy, 1, 103),
		historyTrade(4, 20, 30, DealTypeBuy, DealEntryOutBy, 1, 100),
	}
	got, err := ClosedPositions(deals)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].PositionID != 10 || got[1].PositionID != 20 || got[1].Type != PositionTypeSell {
		t.Fatalf("positions = %v", got)
	}
}

func TestClosedPositionsSortsWithoutMutatingInput(t *testing.T) {
	first := historyTrade(1, 40, 10, DealTypeBuy, DealEntryIn, 1, 100)
	last := historyTrade(2, 40, 10, DealTypeSell, DealEntryOut, 1, 110)
	deals := []Deal{last, {Ticket: 3, Type: DealTypeBalance, Profit: 1000}, first}
	original := append([]Deal(nil), deals...)
	got, err := ClosedPositions(deals)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Deals) != 2 || got[0].Deals[0].Ticket != 1 {
		t.Fatalf("positions = %v", got)
	}
	if !reflect.DeepEqual(deals, original) {
		t.Fatal("input was reordered or changed")
	}
	got[0].Deals[0].Symbol = "changed"
	if !reflect.DeepEqual(deals, original) {
		t.Fatal("result aliases input slice")
	}
}

func TestClosedPositionsRejectsIncompleteAndInconsistentHistory(t *testing.T) {
	in := historyTrade(1, 40, 10, DealTypeBuy, DealEntryIn, 1, 100)
	out := historyTrade(2, 40, 20, DealTypeSell, DealEntryOut, 1, 110)
	changed := func(d Deal, mutate func(*Deal)) Deal { mutate(&d); return d }
	tests := []struct {
		name  string
		deals []Deal
	}{
		{"missing opener", []Deal{out}},
		{"missing opener for reversal", []Deal{changed(out, func(d *Deal) { d.Entry = DealEntryInOut; d.Volume = 2 })}},
		{"exit exceeds open", []Deal{in, changed(out, func(d *Deal) { d.Volume = 2 })}},
		{"exit same direction", []Deal{in, changed(out, func(d *Deal) { d.Type = DealTypeBuy })}},
		{"entry opposite direction", []Deal{in, changed(out, func(d *Deal) { d.Entry = DealEntryIn })}},
		{"reversal too small", []Deal{in, changed(out, func(d *Deal) { d.Entry = DealEntryInOut })}},
		{"symbol mismatch", []Deal{in, changed(out, func(d *Deal) { d.Symbol = "GBPUSD" })}},
		{"duplicate ticket", []Deal{in, changed(out, func(d *Deal) { d.Ticket = in.Ticket })}},
		{"reused position", []Deal{in, out, changed(in, func(d *Deal) { d.Ticket = 3; d.Time = 30 })}},
		{"unknown entry", []Deal{in, changed(out, func(d *Deal) { d.Entry = 99 })}},
		{"missing identifier", []Deal{changed(in, func(d *Deal) { d.PositionID = 0 })}},
		{"zero volume", []Deal{changed(in, func(d *Deal) { d.Volume = 0 })}},
		{"negative volume", []Deal{changed(in, func(d *Deal) { d.Volume = -1 })}},
		{"fractional wire unit", []Deal{changed(in, func(d *Deal) { d.Volume = 0.000000015 })}},
		{"nonfinite volume", []Deal{changed(in, func(d *Deal) { d.Volume = math.NaN() })}},
		{"nonfinite price", []Deal{changed(in, func(d *Deal) { d.PriceOpen = math.Inf(1) })}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ClosedPositions(tt.deals)
			var historyErr *PositionHistoryError
			if !errors.As(err, &historyErr) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestHistoryPositionsFetchesOpeningDealsBeforeWindow(t *testing.T) {
	// The opener is deliberately earlier than the requested closing interval.
	opened, closed := int64(1700000000), int64(1700000100)
	first, last := buildDeal(t, 1, "EURUSD", "open", 0.1), buildDeal(t, 2, "EURUSD", "close", 0.1)
	set := func(raw []byte, index int, v uint64) {
		pos := 0
		for _, f := range dealSchema[:index] {
			pos += f.size()
		}
		switch dealSchema[index].size() {
		case 4:
			binary.LittleEndian.PutUint32(raw[pos:], uint32(v))
		case 8:
			binary.LittleEndian.PutUint64(raw[pos:], v)
		default:
			t.Fatalf("unsupported test field %d", index)
		}
	}
	set(first, 3, uint64(opened))
	set(first, 6, uint64(DealTypeBuy))
	set(first, 7, uint64(DealEntryIn))
	set(first, 25, 0)
	set(last, 3, uint64(closed))
	set(last, 6, uint64(DealTypeSell))
	set(last, 7, uint64(DealEntryOut))
	set(last, 25, 0)
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, 2)
	body = append(body, first...)
	body = append(body, last...)
	body = append(body, 0, 0, 0, 0)
	from, to := time.Unix(closed, 0), time.Unix(closed, 0)
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		cmd, payload, err := p.read()
		if err != nil {
			t.Error(err)
			return
		}
		if cmd != cmdTradeHistory || len(payload) != 8 || binary.LittleEndian.Uint32(payload) != 0 || binary.LittleEndian.Uint32(payload[4:]) != uint32(to.Unix()+1) {
			t.Errorf("history request command %d payload %x", cmd, payload)
		}
		if err := p.reply(cmd, 0, body); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = p.read()
	})
	positions, err := c.HistoryPositions(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 1 || positions[0].TimeOpen != opened*1000 || positions[0].TimeClose != closed*1000 {
		t.Fatalf("positions = %+v", positions)
	}
}

func TestHistoryPositionsFiltersFinalCloseTime(t *testing.T) {
	// Exercise local filtering independently of the server's date selection.
	raw := func(ticket uint64, sec uint32, typ DealType, entry DealEntry, position uint64) []byte {
		r := buildDeal(t, ticket, "EURUSD", "", 1)
		pos := 0
		for i, f := range dealSchema {
			switch i {
			case 3:
				binary.LittleEndian.PutUint32(r[pos:], sec)
			case 6:
				binary.LittleEndian.PutUint32(r[pos:], uint32(typ))
			case 7:
				binary.LittleEndian.PutUint32(r[pos:], uint32(entry))
			case 19:
				binary.LittleEndian.PutUint64(r[pos:], position)
			case 25:
				binary.LittleEndian.PutUint32(r[pos:], 0)
			}
			pos += f.size()
		}
		return r
	}
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, 6)
	for _, r := range [][]byte{raw(1, 10, DealTypeBuy, DealEntryIn, 1), raw(2, 20, DealTypeSell, DealEntryOut, 1), raw(3, 10, DealTypeBuy, DealEntryIn, 2), raw(4, 30, DealTypeSell, DealEntryOut, 2), raw(5, 10, DealTypeBuy, DealEntryIn, 3), raw(6, 40, DealTypeSell, DealEntryOut, 3)} {
		body = append(body, r...)
	}
	body = append(body, 0, 0, 0, 0)
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
		_, _, _ = p.read()
	})
	got, err := c.HistoryPositions(context.Background(), time.Unix(25, 0), time.Unix(35, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PositionID != 2 {
		t.Fatalf("filtered positions = %v", got)
	}
}

func TestHistoryPositionsValidatesTimeBounds(t *testing.T) {
	c := &Client{}
	for _, end := range []time.Time{time.Unix(-1, 0), time.Unix(0, 0), time.Unix(math.MaxInt32, 0), time.Unix(math.MaxInt32+1, 0)} {
		if _, err := c.HistoryPositions(context.Background(), time.Time{}, end); err == nil {
			t.Errorf("invalid upper bound accepted: %v", end)
		}
	}
	if _, err := c.HistoryPositions(context.Background(), time.Unix(20, 0), time.Unix(10, 0)); err == nil {
		t.Fatal("reversed interval accepted")
	}
}

func TestHistoryPositionsPropagatesStatusError(t *testing.T) {
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		cmd, _, err := p.read()
		if err != nil {
			t.Error(err)
			return
		}
		if err := p.reply(cmd, 9, nil); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = p.read()
	})
	_, err := c.HistoryPositions(context.Background(), time.Time{}, time.Time{})
	var status *StatusError
	if !errors.As(err, &status) || status.Command != cmdTradeHistory || status.Code != 9 {
		t.Fatalf("history status error = %v", err)
	}
}

func TestHistoryPositionsHonorsSubsecondBounds(t *testing.T) {
	first, last := buildDeal(t, 1, "EURUSD", "", 1), buildDeal(t, 2, "EURUSD", "", 1)
	set := func(raw []byte, sec uint32, entry DealEntry, typ DealType, msec uint32) {
		pos := 0
		for i, f := range dealSchema {
			switch i {
			case 3:
				binary.LittleEndian.PutUint32(raw[pos:], sec)
			case 6:
				binary.LittleEndian.PutUint32(raw[pos:], uint32(typ))
			case 7:
				binary.LittleEndian.PutUint32(raw[pos:], uint32(entry))
			case 25:
				binary.LittleEndian.PutUint32(raw[pos:], msec)
			}
			pos += f.size()
		}
	}
	set(first, 10, DealEntryIn, DealTypeBuy, 0)
	set(last, 20, DealEntryOut, DealTypeSell, 500)
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, 2)
	body = append(body, first...)
	body = append(body, last...)
	body = append(body, 0, 0, 0, 0)
	closed := time.Unix(20, 500000000)
	tests := []struct {
		name     string
		from, to time.Time
		want     int
	}{
		{"inclusive instant", closed, closed, 1},
		{"after close by nanosecond", closed.Add(time.Nanosecond), closed.Add(time.Second), 0},
		{"before close by nanosecond", time.Time{}, closed.Add(-time.Nanosecond), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
				cmd, payload, err := p.read()
				if err != nil {
					t.Error(err)
					return
				}
				if len(payload) != 8 || binary.LittleEndian.Uint32(payload[4:]) != uint32(tt.to.Unix()+1) {
					t.Errorf("history upper bound %x", payload)
				}
				if err := p.reply(cmd, 0, body); err != nil {
					t.Error(err)
					return
				}
				_, _, _ = p.read()
			})
			positions, err := c.HistoryPositions(context.Background(), tt.from, tt.to)
			if err != nil {
				t.Fatal(err)
			}
			if len(positions) != tt.want {
				t.Fatalf("got %d positions, want %d", len(positions), tt.want)
			}
		})
	}
}
