package webterm

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestLiveBootstrap talks to the public web terminal. The handshake needs
// no credentials, so it is safe to run.
//
//	MT5_WEB_LIVE=1 go test ./webterm -run TestLiveBootstrap -v
func TestLiveBootstrap(t *testing.T) {
	if os.Getenv("MT5_WEB_LIVE") == "" {
		t.Skip("set MT5_WEB_LIVE=1 to talk to the live server")
	}
	url := os.Getenv("MT5_WEB_URL")
	if url == "" {
		url = DefaultURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, err := Dial(ctx, Options{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	t.Logf("server build %d", c.TerminalBuild())
	if c.TerminalBuild() == 0 {
		t.Fatal("the server did not report a build number")
	}
	if len(c.Token()) != 64 {
		t.Fatalf("token is %d bytes, want 64", len(c.Token()))
	}
}

// TestLiveHistory logs in with the credentials from the environment and
// reads account state, trade history, open positions and closed-position history.
//
//	MT5_WEB_LIVE=1 MT5_LOGIN=123456 MT5_PASSWORD=... \
//	  go test ./webterm -run TestLiveHistory -v
func TestLiveHistory(t *testing.T) {
	if os.Getenv("MT5_WEB_LIVE") == "" {
		t.Skip("set MT5_WEB_LIVE=1 to talk to the live server")
	}
	login := os.Getenv("MT5_LOGIN")
	password := os.Getenv("MT5_PASSWORD")
	if login == "" || password == "" {
		t.Skip("need MT5_LOGIN and MT5_PASSWORD")
	}
	var number uint64
	for _, c := range login {
		if c < '0' || c > '9' {
			t.Fatalf("MT5_LOGIN is not a number: %q", login)
		}
		number = number*10 + uint64(c-'0')
	}

	url := os.Getenv("MT5_WEB_URL")
	if url == "" {
		url = DefaultURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := Dial(ctx, Options{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Login(ctx, LoginOptions{Login: number, Password: password, Host: os.Getenv("MT5_HOST")}); err != nil {
		t.Fatal(err)
	}
	t.Log("login succeeded")

	acc, err := c.Account(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("server=%s investor=%v", acc.Server, acc.Investor)

	to := time.Now().Add(24 * time.Hour)
	from := to.AddDate(0, 0, -90)
	h, err := c.History(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("history: %d deals, %d orders", len(h.Deals), len(h.Orders))

	t.Run("OpenPositions", func(t *testing.T) {
		positions, err := c.Positions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("open positions: %d", len(positions))
	})
	t.Run("SLTPHistory", func(t *testing.T) {
		observations, err := c.HistorySLTP(ctx, from, to)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("SL/TP historical observations: %d", len(observations))
		for _, obs := range observations {
			if obs.Kind != "history" || obs.Previous != nil || obs.ObservedAt == 0 {
				t.Fatalf("invalid historical observation metadata: source=%s kind=%s", obs.Source, obs.Kind)
			}
		}
	})
	t.Run("SLTPSnapshot", func(t *testing.T) {
		stop := errors.New("snapshot collected")
		err := c.WatchSLTP(ctx, time.Second, func(_ time.Time, observations []SLTPObservation) error {
			t.Logf("SL/TP live baselines: %d", len(observations))
			for _, obs := range observations {
				if obs.Kind != "baseline" || obs.Previous != nil {
					t.Fatal("first snapshot must contain only baselines")
				}
			}
			return stop
		})
		if !errors.Is(err, stop) {
			t.Fatalf("SL/TP snapshot: %v", err)
		}
	})
	t.Run("ClosedPositions", func(t *testing.T) {
		positions, err := c.HistoryPositions(ctx, from, to)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range positions {
			if p.TimeClose < p.TimeOpen || len(p.Deals) < 2 {
				t.Fatalf("invalid reconstructed position %d", p.PositionID)
			}
		}
		t.Logf("closed positions in history window: %d", len(positions))
	})
}
