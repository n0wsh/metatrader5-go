package mt5legacy

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// serveHistory answers deal/order range requests; dealsAt returns how many
// deals the terminal has downloaded by the n-th deals poll.
func serveHistory(f *fakeTerminal, dealsAt func(poll int) int) {
	poll := 0
	for {
		req, err := f.tryReadRequest()
		if err != nil {
			return
		}
		switch binary.LittleEndian.Uint32(req) {
		case cmdHistoryDealsByRange:
			n := dealsAt(poll)
			poll++
			payload := [][]byte{le32(uint32(n))}
			for i := 0; i < n; i++ {
				payload = append(payload, dealRecord(uint64(i+1), "EURUSD", 1))
			}
			f.reply(cmdHistoryDealsByRange, 1, payload...)
		case cmdHistoryOrdersByRange:
			f.reply(cmdHistoryOrdersByRange, 1, le32(1), orderRecord(9, "EURUSD"))
		}
	}
}

func TestWaitHistorySynced(t *testing.T) {
	c, f := newTestClient(t)
	// Download in progress: 0, 2, 5 deals, then complete at 7.
	go serveHistory(f, func(poll int) int { return []int{0, 2, 5, 7}[min(poll, 3)] })

	opts := SyncOptions{Interval: 5 * time.Millisecond, StableFor: 40 * time.Millisecond}
	h, err := c.WaitHistorySynced(context.Background(), time.Unix(0, 0), time.Now(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Deals) != 7 || len(h.Orders) != 1 {
		t.Fatalf("got %d deals, %d orders; want 7, 1", len(h.Deals), len(h.Orders))
	}
}

func TestWaitHistorySyncedTimeout(t *testing.T) {
	c, f := newTestClient(t)
	// Never settles.
	go serveHistory(f, func(poll int) int { return poll % 50 })

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	opts := SyncOptions{Interval: 5 * time.Millisecond, StableFor: time.Second}
	h, err := c.WaitHistorySynced(ctx, time.Unix(0, 0), time.Now(), opts)
	var e *Error
	if !errors.As(err, &e) || e.Code != ResInternalFailTimeout {
		t.Fatalf("err = %v", err)
	}
	if len(h.Orders) != 1 {
		t.Fatalf("want partial snapshot, got %+v", h)
	}
}
