# metatrader5 (Go)

[![Lint and Testing](https://github.com/n0wsh/metatrader5-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/n0wsh/metatrader5-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/n0wsh/metatrader5-go/webterm.svg)](https://pkg.go.dev/github.com/n0wsh/metatrader5-go/webterm)

Go client for the **MetaTrader 5 web terminal** protocol: log in with an account
(typically the investor / read-only password) and read open positions, deal/order history, and closed-position history.

One dependency (`gorilla/websocket`), no cgo, no build tags, no local terminal.
Cross-compiles to `linux/amd64`, `linux/arm64`, `linux/386`, `linux/arm`,
`darwin/*` and `windows/*` from the same source.

## Install

```sh
go get github.com/n0wsh/metatrader5-go/webterm
```

Read the [API documentation on pkg.go.dev](https://pkg.go.dev/github.com/n0wsh/metatrader5-go/webterm).

## Usage

Replace `mt5.broker.example` with your broker's actual web terminal host.

```go
import "github.com/n0wsh/metatrader5-go/webterm"

c, err := webterm.Dial(ctx, webterm.Options{URL: "wss://mt5.broker.example/terminal"})
if err != nil { ... }
defer c.Close()

if _, err := c.Login(ctx, webterm.LoginOptions{
    Login:    12345678,
    Password: investorPassword,
}); err != nil { ... }

h, err := c.History(ctx, from, to)      // h.Deals, h.Orders
acc, err := c.Account(ctx)              // name, server, balance, permissions
open, err := c.Positions(ctx)          // current open-position snapshots
closed, err := c.HistoryPositions(ctx, from, to) // fully closed lifetimes
```

```sh
MT5_PASSWORD=... go run ./cmd/mt5web -url wss://mt5.broker.example/terminal -login 12345678 -days 90 -positions -history-positions
```

## SL/TP observations

`HistorySLTP(ctx, from, to)` returns all available SL/TP observations from deal
and order history. Use `SLTPHistory(h)` for already loaded history. Each entry
preserves its source, ticket, position identifier, symbol, levels, and record
time. Orders without a position identifier are retained by order ticket.
Matching order/deal values remain separate evidence records.

These records are observations, not a complete modification log. A deal's
levels describe its execution; a historical order's levels describe the returned
order record. Order completion/setup times are labeled explicitly and do not
establish when its SL/TP last changed. See MetaQuotes' [deal properties](https://www.mql5.com/en/docs/constants/tradingconstants/dealproperties)
and [order properties](https://www.mql5.com/en/docs/constants/tradingconstants/orderproperties).

`WatchSLTP(ctx, interval, handler)` polls both open positions and pending orders.
It emits a baseline when an entity first appears, followed by changes with
previous and new levels, including removal of a level (zero). The handler runs
on every successful snapshot, including empty batches as coverage heartbeats.
Return a storage error from the handler to stop the recorder. `OpenOrders(ctx)`
also exposes the pending-order snapshot independently.

```go
err := c.WatchSLTP(ctx, time.Second, func(at time.Time, observations []webterm.SLTPObservation) error {
    return encoder.Encode(struct {
        ObservedAt   int64                     `json:"observed_at"`
        Observations []webterm.SLTPObservation `json:"observations"`
    }{at.UnixMilli(), observations})
})
```

Snapshot timestamps are local observation times, while record timestamps use
broker time. Differences establish that values changed between observations;
multiple changes between polls can be missed. Historical observations are not
used as live baselines. Snapshot/parse failures stop recording and are returned,
including unsupported position reply layouts. Restart after a gap establishes
new baselines. Disappearance does not prove closure, and intermediate changes
before recording or during disconnects cannot be recovered this way.

For a custom polling loop, the zero-value `SLTPTracker` accepts complete position
and pending-order snapshots through `Observe`; keep one tracker per account and
call `Reset` after a recording gap.

The `mt5sltp` CLI exports all available history by default. With `-watch`, it
continues recording snapshot differences. Its JSON Lines output includes session,
history, and snapshot records identified by account and endpoint. `-output`
appends to a file and syncs each batch; a new file has owner-only permissions.
Every invocation starts a new session and baseline; history exports may repeat
across runs. Use source and ticket when reconciling repeated historical records.

```sh
# Historical observations only; -from/-to accept RFC 3339 timestamps.
MT5_PASSWORD=... go run ./cmd/mt5sltp -url wss://mt5.broker.example/terminal -login 12345678

# History followed by ongoing recording; stop with Ctrl-C.
MT5_PASSWORD=... go run ./cmd/mt5sltp -url wss://mt5.broker.example/terminal -login 12345678 -watch 1s -output sltp.jsonl
```

## Layout

```
webterm/          public client API, transport, protocol, and records
cmd/mt5web/       command line: login and dump the history as JSON
legacy/pipe/      the desktop terminal pipe client, a separate module, see below
```

`legacy/pipe` is a separate Go module, so it is not built, vetted or tested by
the commands above. It talks to `terminal64.exe` over a Windows named pipe and
needs Windows or Wine. Delete the directory if you do not want it.

## Client lifecycle and errors

Calls on a client are serialized because replies identify only their command,
not an individual request. Log in before starting concurrent account, history,
or ping calls; do not overlap Login or Logout with other operations.

`Options.Timeout` (30 seconds by default) bounds the entire command, including
waiting for another command, writing, and receiving the reply. The caller's
context can impose a shorter deadline. Canceling a queued call leaves the
connection usable. Once a command starts, cancellation or timeout closes the
connection so that its late reply cannot be mistaken for a subsequent reply.
Dial and log in again to continue; commands are not retried automatically.
`Close` is idempotent and wakes active and queued callers.

Nonzero terminal statuses return `*webterm.StatusError`:

```go
var status *webterm.StatusError
if errors.As(err, &status) {
    // Command and Code are the numeric values from the terminal protocol.
    log.Printf("command %d rejected with status %d", status.Command, status.Code)
}
```

Use `errors.Is` with `context.Canceled`, `context.DeadlineExceeded`, or
`webterm.ErrClosed` to identify cancellation, timeout, or explicit closure.

## Positions

`Positions(ctx)` returns open positions as a fresh snapshot, with direction,
volume in lots, open/current prices, stop loss, take profit, profit, swap, and
identifiers. It excludes pending orders. Price and profit fields are the values
returned by the terminal; this client does not stream ticks or continuously
recalculate them. Times use Unix milliseconds, like `Deal`.

`HistoryPositions(ctx, from, to)` returns only positions whose final close falls
inside the inclusive interval, sorted by closing time then `PositionID`. A zero
start has no lower limit; a zero end means the terminal's current time. It fetches
all available deals through the end of the interval so opening deals before
`from` are retained. This can cost more time and memory than a deal-history
query for the same date window. Broker-time considerations below still apply.

Positions are grouped by `PositionID`. Partial closes leave the lifetime open,
and netting reversals keep the same lifetime until the remaining volume reaches
zero. `Type` is the initial direction; `Volume` totals opening lots, including
new exposure on reversals. Entry and exit prices are weighted by executed
volume. `Profit` is summed from the terminal's deals, and `NetProfit` adds
commission, swap, and fees on those trading deals. Separately booked balance or
commission operations are excluded. The underlying `Deals` remain available.
This differs from the terminal's history view, which can show partial closures.

For deals already loaded, use `webterm.ClosedPositions(deals)` without another
request. Supply complete lifetimes, including their opening deals. Detected
missing openers or inconsistent transitions return `*webterm.PositionHistoryError`
rather than an invented result. Archived deals can make reconstruction
impossible; paired missing records cannot always be detected. Reopening an
already flat position under the same identifier (for example, some service or
correction operations) is rejected rather than merged. Nonzero history end
times must fit the protocol's signed 32-bit Unix-seconds range, with one second
reserved for inclusive boundary overfetch.

The CLI's `-positions` and `-history-positions` flags add `positions` and
`history_positions` arrays to its JSON output; `-group` filters them by symbol.

## How it works

The MetaTrader web terminal is a browser application: the page loads a JavaScript
bundle that talks to a MetaQuotes hosted terminal over a binary WebSocket. The
broker's terminal holds the broker connection, so this client never speaks the
broker protocol itself.

```
your Go service ──(wss)──► wss://mt5.broker.example/terminal ──► terminal ──► broker
```

The protocol was recovered from that bundle (build 6316) and verified against a
live broker account.

### Wire format (little-endian throughout)

```
websocket message := u32 body_len | u32 version | AES-CBC(ciphertext)
ciphertext        := AES-CBC(command frame), zero IV, PKCS#7 padding
command frame     := u8 random | u8 random | u16 command | payload
response frame    := u8 random | u8 random | u16 command | u8 status | body
```

The first command is the bootstrap (command 0): a 64 byte token encrypted with the
key that ships inside the bundle, obfuscated with a one character shift
(`code - 1`, with `28` and `23` standing for `&` and `!`). The reply carries the
server build, a session token and the session key that all later traffic is
encrypted with.

The server answers with `version = 0` while the client sends `version = 1`; the
field is ignored on receipt, like the browser client does.

### Commands

| cmd | name | request | response |
|-----|------|---------|----------|
| `0` | bootstrap | 64 byte token | `u16 build`, 64 byte token, AES session key |
| `28` | login | login, password, host, session, otp | 160 byte token, `u64 session` |
| `3` | account | none | account summary |
| `4` | open positions/orders | none | `u32 n`, `n × Position(344 B)`, `u32 n`, `n × Order(356 B)` |
| `5` | trade history | `i32 from`, `i32 to` | `u32 n`, `n × Deal(356 B)`, `u32 n`, `n × Order(356 B)` |
| `51` | ping | none | none |

The login request is 920 bytes and its layout matters: the length of the host
string has to match the string, a mismatch makes the server drop the socket
instead of answering. The host is the hostname of the page that linked to the
terminal, not the trade server name; it defaults to the host of the URL.

### Records

Deals and orders are the same records the desktop terminal returns, with two
differences: the volume is the number of lots multiplied by `1e8` (the desktop
pipe uses `1e4` and a float), and the times are split into a `u32` seconds part
and an `i32` milliseconds part. The schemas are in `webterm/schemas.go`; fixture provenance is in
`webterm/testdata/README.md`.

Times are broker server time, so widen the window by a day and filter afterwards.

## Finding a broker's endpoint

Brokers that offer a web terminal may use different hosts for each trade server.
Check your broker's web terminal landing page for its server-to-URL map.
For example, a map might look like:

```json
{"api-get-servers": [
  {"serverName": "Broker-MT5",   "serverURL": "https://mt5.broker.example/terminal"},
  {"serverName": "Broker-MT5 2", "serverURL": "https://mt5-2.broker.example/terminal"}
]}
```

The WebSocket endpoint is the same URL with `https` replaced by `wss`.

Not every broker runs one. Where there is no web terminal, the only route is a
desktop terminal on Windows, which is what `legacy/pipe` is for.

## Tests

```sh
go test ./...                                   # unit and in-memory WebSocket tests
go test -race ./...                              # concurrency checks
go vet ./...
MT5_WEB_LIVE=1 go test ./webterm -run TestLiveBootstrap -v     # handshake only
MT5_WEB_LIVE=1 MT5_LOGIN=... MT5_PASSWORD=... \
  go test ./webterm -run TestLiveHistory -v                    # login and history
```

## Linting

CI runs golangci-lint v2.15.0 with the standard linters for both Go modules
on Linux, plus the legacy module's Windows pipe and relay code.
After installing that version, run:

```sh
golangci-lint run ./...
(cd legacy/pipe && golangci-lint run --config ../../.golangci.yml ./...)
(cd legacy/pipe && GOOS=windows golangci-lint run --config ../../.golangci.yml ./...)
```

## License

The original code in this repository is available under the [MIT License](LICENSE).
Third-party dependencies retain their own licenses. This license does not grant
rights to MetaQuotes software or services.
