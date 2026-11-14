// mt5sltp exports available historical SL/TP evidence and optionally records
// observed changes to positions and pending orders as appendable JSON Lines.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/n0wsh/metatrader5-go/webterm"
)

type record struct {
	Kind         string                    `json:"kind"`
	Login        uint64                    `json:"login"`
	URL          string                    `json:"url"`
	ObservedAt   int64                     `json:"observed_at"`
	Observations []webterm.SLTPObservation `json:"observations"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (runErr error) {
	url := flag.String("url", webterm.DefaultURL, "web terminal endpoint")
	login := flag.Uint64("login", 0, "account number")
	host := flag.String("host", "", "login host (defaults to endpoint host)")
	from := flag.String("from", "", "history start, RFC 3339 (default: all available)")
	to := flag.String("to", "", "history end, RFC 3339 (default: terminal now)")
	watch := flag.Duration("watch", 0, "poll interval after history export, e.g. 1s (0: history only)")
	group := flag.String("group", "", "symbol filter, e.g. *USD*,!EUR*")
	output := flag.String("output", "", "append JSON Lines to this file (default: stdout)")
	flag.Parse()
	if *login == 0 || os.Getenv("MT5_PASSWORD") == "" {
		return errors.New("need -login and MT5_PASSWORD")
	}
	if *watch < 0 {
		return errors.New("-watch must be nonnegative")
	}
	var start, end time.Time
	var err error
	if *from != "" {
		start, err = time.Parse(time.RFC3339, *from)
		if err != nil {
			return fmt.Errorf("-from: %w", err)
		}
	}
	if *to != "" {
		end, err = time.Parse(time.RFC3339, *to)
		if err != nil {
			return fmt.Errorf("-to: %w", err)
		}
	}
	if !start.IsZero() && !end.IsZero() && start.After(end) {
		return errors.New("-from must not be after -to")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := webterm.Dial(ctx, webterm.Options{URL: *url})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Login(ctx, webterm.LoginOptions{Login: *login, Password: os.Getenv("MT5_PASSWORD"), Host: *host}); err != nil {
		return err
	}
	var writer io.Writer = os.Stdout
	var file *os.File
	if *output != "" {
		file, err = os.OpenFile(*output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, file.Close()) }()
		writer = file
	}
	enc := json.NewEncoder(writer)
	write := func(kind string, at time.Time, observations []webterm.SLTPObservation) error {
		filtered := make([]webterm.SLTPObservation, 0, len(observations))
		for _, obs := range observations {
			if *group == "" || webterm.MatchGroup(*group, obs.Symbol) {
				filtered = append(filtered, obs)
			}
		}
		if err := enc.Encode(record{Kind: kind, Login: *login, URL: *url, ObservedAt: at.UnixMilli(), Observations: filtered}); err != nil {
			return err
		}
		if file != nil {
			return file.Sync()
		}
		return nil
	}
	// Every invocation starts a new session; never infer continuity across runs.
	if err := write("session", time.Now(), nil); err != nil {
		return err
	}
	history, err := c.HistorySLTP(ctx, start, end)
	if err != nil {
		return err
	}
	if err := write("history", time.Now(), history); err != nil {
		return err
	}
	if *watch == 0 {
		return nil
	}
	err = c.WatchSLTP(ctx, *watch, func(at time.Time, observations []webterm.SLTPObservation) error {
		return write("snapshot", at, observations)
	})
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return err
}
