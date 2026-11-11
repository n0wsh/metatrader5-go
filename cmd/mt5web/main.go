// mt5web logs into the MetaTrader 5 web terminal (typically with the
// investor, read-only password) and prints its trade history as JSON.
//
//	MT5_PASSWORD=... mt5web -login 123456 -days 90
//
// Brokers that run their own web terminal use their own endpoint:
//
//	MT5_PASSWORD=... mt5web -url wss://mt5.example.com/terminal -login 123456
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"github.com/n0wsh/metatrader5-go/webterm"
)

func main() {
	url := flag.String("url", webterm.DefaultURL, "web terminal endpoint")
	login := flag.Uint64("login", 0, "account number")
	host := flag.String("host", "", "web terminal host, e.g. web.metatrader.app (defaults to the host of -url)")
	days := flag.Int("days", 90, "history window in days")
	from := flag.String("from", "", "history start, RFC 3339 (overrides -days)")
	to := flag.String("to", "", "history end, RFC 3339")
	group := flag.String("group", "", `symbol filter, e.g. "*USD*,!EUR*"`)
	showAccount := flag.Bool("account", false, "print the account summary too")
	showPositions := flag.Bool("positions", false, "include current open positions")
	showHistoryPositions := flag.Bool("history-positions", false, "include fully closed positions (fetches all available opening deals)")
	flag.Parse()

	password := os.Getenv("MT5_PASSWORD")
	if *login == 0 || password == "" {
		log.Fatal("need -login and MT5_PASSWORD")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	c, err := webterm.Dial(ctx, webterm.Options{URL: *url})
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer c.Close()
	log.Printf("connected, server build %d", c.TerminalBuild())

	if _, err := c.Login(ctx, webterm.LoginOptions{
		Login:    *login,
		Password: password,
		Host:     *host,
	}); err != nil {
		log.Fatalf("login: %v", err)
	}
	log.Printf("logged in, session %d", c.Session())

	out := struct {
		Account          *webterm.Account          `json:"account,omitempty"`
		History          *webterm.History          `json:"history"`
		Positions        *[]webterm.Position       `json:"positions,omitempty"`
		HistoryPositions *[]webterm.ClosedPosition `json:"history_positions,omitempty"`
	}{}

	if *showAccount {
		acc, err := c.Account(ctx)
		if err != nil {
			log.Fatalf("account: %v", err)
		}
		out.Account = acc
		log.Printf("account %s (%s), balance %.2f %s", acc.Name, acc.Server, acc.Balance, acc.Currency)
	}

	fromTime, toTime, err := window(*from, *to, *days)
	if err != nil {
		log.Fatal(err)
	}
	h, err := c.History(ctx, fromTime, toTime)
	if err != nil {
		log.Fatalf("history: %v", err)
	}
	if *group != "" {
		h.Deals = filterDeals(h.Deals, *group)
		h.Orders = filterOrders(h.Orders, *group)
	}
	log.Printf("history: %d deals, %d orders", len(h.Deals), len(h.Orders))
	out.History = h

	if *showPositions {
		positions, err := c.Positions(ctx)
		if err != nil {
			log.Fatalf("positions: %v", err)
		}
		if *group != "" {
			filtered := positions[:0]
			for _, p := range positions {
				if webterm.MatchGroup(*group, p.Symbol) {
					filtered = append(filtered, p)
				}
			}
			positions = filtered
		}
		out.Positions = &positions
	}
	if *showHistoryPositions {
		positions, err := c.HistoryPositions(ctx, fromTime, toTime)
		if err != nil {
			log.Fatalf("position history: %v", err)
		}
		if *group != "" {
			filtered := positions[:0]
			for _, p := range positions {
				if webterm.MatchGroup(*group, p.Symbol) {
					filtered = append(filtered, p)
				}
			}
			positions = filtered
		}
		out.HistoryPositions = &positions
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}

// window resolves the history period. The terminal compares the range
// against server time, so the end is padded by a day to cover brokers that
// run ahead of UTC.
func window(from, to string, days int) (time.Time, time.Time, error) {
	var start, end time.Time
	switch {
	case from != "":
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return start, end, err
		}
		start = t
	default:
		end = time.Now().Add(24 * time.Hour)
		start = end.AddDate(0, 0, -days-1)
	}
	if to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return start, end, err
		}
		end = t
	}
	return start, end, nil
}

// filterDeals keeps the deals matching a symbol filter such as
// "*USD*,!EUR*".
func filterDeals(deals []webterm.Deal, group string) []webterm.Deal {
	out := deals[:0]
	for _, d := range deals {
		if webterm.MatchGroup(group, d.Symbol) {
			out = append(out, d)
		}
	}
	return out
}

// filterOrders keeps the orders matching a symbol filter.
func filterOrders(orders []webterm.Order, group string) []webterm.Order {
	out := orders[:0]
	for _, o := range orders {
		if webterm.MatchGroup(group, o.Symbol) {
			out = append(out, o)
		}
	}
	return out
}
