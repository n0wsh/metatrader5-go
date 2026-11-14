// mt5history logs a terminal into an account (typically with the investor,
// read-only password) and prints its deal and order history as JSON.
//
//	MT5_PASSWORD=... mt5history -relay 127.0.0.1:5555 -login 123456 -server Broker-Live -days 90
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	mt5 "github.com/n0wsh/metatrader5-go/legacy/pipe"
)

func main() {
	relay := flag.String("relay", "", "mt5relay TCP address")
	terminal := flag.String("terminal", "", "path to terminal64.exe (Windows only)")
	pipe := flag.String("pipe", "", "terminal pipe name (Windows only)")
	login := flag.Uint64("login", 0, "account number")
	server := flag.String("server", "", "trade server name, e.g. Broker-Live")
	days := flag.Int("days", 90, "history window in days")
	group := flag.String("group", "", `symbol filter, e.g. "*USD*,!EUR*"`)
	flag.Parse()

	password := os.Getenv("MT5_PASSWORD")
	if *login == 0 || *server == "" || password == "" {
		log.Fatal("need -login, -server and MT5_PASSWORD")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var (
		c   *mt5.Client
		err error
	)
	switch {
	case *relay != "":
		c, err = mt5.DialTCP(ctx, *relay)
	case *pipe != "":
		c, err = mt5.DialPipe(ctx, *pipe)
	case *terminal != "":
		c, err = mt5.DialTerminal(ctx, *terminal, mt5.TerminalOptions{Launch: true})
	default:
		log.Fatal("need one of -relay, -pipe, -terminal")
	}
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Close() }()
	log.Printf("connected, terminal build %d", c.TerminalBuild())

	loginCtx, cancelLogin := context.WithTimeout(ctx, mt5.DefaultLoginTimeout)
	err = c.Login(loginCtx, *login, password, *server)
	cancelLogin()
	if err != nil {
		log.Fatalf("login: %v", err)
	}

	// MT5 filters by server time; padding "to" by a day covers brokers ahead of UTC.
	to := time.Now().Add(24 * time.Hour)
	from := to.AddDate(0, 0, -*days-1)
	// Authorized doesn't mean downloaded: wait until the history stops growing.
	h, err := c.WaitHistorySynced(ctx, from, to, mt5.SyncOptions{Group: *group})
	if err != nil {
		log.Fatalf("history: %v", err)
	}
	log.Printf("synced: %d deals, %d orders", len(h.Deals), len(h.Orders))

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(h); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}
