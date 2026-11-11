package mt5legacy

import (
	"context"
	"errors"
	"time"
)

// Defaults for WaitHistorySynced.
const (
	DefaultSyncInterval  = 500 * time.Millisecond
	DefaultSyncStableFor = 3 * time.Second
)

// SyncOptions controls WaitHistorySynced. Zero values use the defaults.
type SyncOptions struct {
	// Interval between history polls.
	Interval time.Duration
	// StableFor is how long the history must stay unchanged to count as synced.
	StableFor time.Duration
	// Group is an optional symbol filter, as in HistoryDeals.
	Group string
}

// History is a snapshot of an account's deals and orders.
type History struct {
	Deals  []Deal  `json:"deals"`
	Orders []Order `json:"orders"`
}

type historyFingerprint struct {
	deals, orders       int
	lastDeal, lastOrder uint64
}

func fingerprint(h History) historyFingerprint {
	fp := historyFingerprint{deals: len(h.Deals), orders: len(h.Orders)}
	for _, d := range h.Deals {
		fp.lastDeal = max(fp.lastDeal, d.Ticket)
	}
	for _, o := range h.Orders {
		fp.lastOrder = max(fp.lastOrder, o.Ticket)
	}
	return fp
}

// WaitHistorySynced polls deals and orders in [from, to] until neither the
// counts nor the newest tickets have changed for opts.StableFor, then returns
// that snapshot. Use it after Login: an authorized terminal may still be
// downloading history from the broker.
//
// If ctx ends first, it returns the last snapshot along with the error
// (ResInternalFailTimeout on deadline), so callers can still use partial data.
func (c *Client) WaitHistorySynced(ctx context.Context, from, to time.Time, opts SyncOptions) (History, error) {
	if opts.Interval <= 0 {
		opts.Interval = DefaultSyncInterval
	}
	if opts.StableFor <= 0 {
		opts.StableFor = DefaultSyncStableFor
	}
	t := time.NewTicker(opts.Interval)
	defer t.Stop()

	var (
		last       History
		lastFP     historyFingerprint
		lastChange time.Time
	)
	for first := true; ; first = false {
		deals, err := c.HistoryDeals(ctx, from, to, opts.Group)
		if err != nil {
			return last, syncErr(err)
		}
		orders, err := c.HistoryOrders(ctx, from, to, opts.Group)
		if err != nil {
			return last, syncErr(err)
		}
		last = History{Deals: deals, Orders: orders}
		now := time.Now()
		if fp := fingerprint(last); first || fp != lastFP {
			lastFP, lastChange = fp, now
		} else if now.Sub(lastChange) >= opts.StableFor {
			return last, nil
		}
		select {
		case <-ctx.Done():
			return last, syncErr(ctx.Err())
		case <-t.C:
		}
	}
}

func syncErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return resError(ResInternalFailTimeout)
	}
	return err
}
