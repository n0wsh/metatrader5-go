package mt5legacy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"testing"
	"time"
	"unicode/utf16"
)

// fakeTerminal answers requests on the server end of a net.Pipe.
type fakeTerminal struct {
	t    *testing.T
	conn net.Conn
}

func (f *fakeTerminal) readRequest() []byte {
	f.t.Helper()
	b, err := f.tryReadRequest()
	if err != nil {
		f.t.Fatalf("read request: %v", err)
	}
	return b
}

// tryReadRequest is readRequest for server loops that end when the client
// disconnects.
func (f *fakeTerminal) tryReadRequest() ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(f.conn, hdr[:]); err != nil {
		return nil, err
	}
	b := make([]byte, binary.LittleEndian.Uint32(hdr[:]))
	if _, err := io.ReadFull(f.conn, b); err != nil {
		return nil, err
	}
	return b, nil
}

func (f *fakeTerminal) expect(want []byte) {
	f.t.Helper()
	if got := f.readRequest(); !bytes.Equal(got, want) {
		f.t.Fatalf("request\n got %x\nwant %x", got, want)
	}
}

func (f *fakeTerminal) reply(cmd uint32, ret int32, payload ...[]byte) {
	body := le32(cmd)
	body = append(body, le32(uint32(ret))...)
	for _, p := range payload {
		body = append(body, p...)
	}
	f.conn.Write(append(le32(uint32(len(body))), body...))
}

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func le64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

func wstr(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := le32(uint32(len(u)))
	for _, c := range u {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return b
}

func putW(rec []byte, off int, s string) {
	for i, c := range utf16.Encode([]rune(s)) {
		binary.LittleEndian.PutUint16(rec[off+2*i:], c)
	}
}

func putF(rec []byte, off int, v float64) {
	binary.LittleEndian.PutUint64(rec[off:], math.Float64bits(v))
}

// dealRecord lays out a deal at the offsets read by the Python converter.
func dealRecord(ticket uint64, symbol string, profit float64) []byte {
	r := make([]byte, 300)
	binary.LittleEndian.PutUint64(r[0x00:], ticket)
	binary.LittleEndian.PutUint64(r[0x08:], ticket+1000)   // order
	binary.LittleEndian.PutUint64(r[0x10:], 1700000000)    // time
	binary.LittleEndian.PutUint64(r[0x18:], 1700000000123) // time_msc
	binary.LittleEndian.PutUint32(r[0x20:], uint32(DealTypeSell))
	binary.LittleEndian.PutUint32(r[0x24:], uint32(DealEntryOut))
	binary.LittleEndian.PutUint64(r[0x28:], 77)  // magic
	binary.LittleEndian.PutUint64(r[0x30:], 555) // position_id
	binary.LittleEndian.PutUint32(r[0x38:], uint32(DealReasonTP))
	putF(r, 0x3c, 0.1)
	putF(r, 0x44, 1.0850)
	putF(r, 0x4c, -0.7)
	putF(r, 0x54, -0.12)
	putF(r, 0x5c, profit)
	putF(r, 0x64, 0)
	putW(r, 0x6c, symbol)
	putW(r, 0xac, "[tp 1.08500]")
	putW(r, 0xec, "ext-1")
	return r
}

func orderRecord(ticket uint64, symbol string) []byte {
	r := make([]byte, 340)
	binary.LittleEndian.PutUint64(r[0x00:], ticket)
	binary.LittleEndian.PutUint64(r[0x10:], 1700000000500) // time_setup_msc
	binary.LittleEndian.PutUint32(r[0x30:], uint32(OrderTypeBuyLimit))
	binary.LittleEndian.PutUint32(r[0x3c:], uint32(OrderStateFilled))
	binary.LittleEndian.PutUint64(r[0x48:], 555) // position_id
	binary.LittleEndian.PutUint32(r[0x58:], uint32(OrderReasonExpert))
	putF(r, 0x5c, 0.5)
	putF(r, 0x6c, 1.07)
	putF(r, 0x8c, 1.075) // price_stoplimit
	putW(r, 0x94, symbol)
	putW(r, 0x114, "ext-o")
	return r
}

func newTestClient(t *testing.T) (*Client, *fakeTerminal) {
	t.Helper()
	cli, srv := net.Pipe()
	f := &fakeTerminal{t: t, conn: srv}
	t.Cleanup(func() { srv.Close() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		// u32 cmd=4 | u32 3 | u32 6 | L"Python"
		f.expect(append(append(le32(4), le32(3)...), wstr("Python")...))
		f.reply(4, 1, le32(5430))
	}()
	c, err := NewClient(context.Background(), cli)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	t.Cleanup(func() { c.Close() })
	return c, f
}

func TestHandshake(t *testing.T) {
	c, _ := newTestClient(t)
	if c.TerminalBuild() != 5430 {
		t.Fatalf("build = %d", c.TerminalBuild())
	}
}

func TestLoginInvestor(t *testing.T) {
	c, f := newTestClient(t)
	go func() {
		// u32 cmd=5 | u32 0 | u64 login | password | server
		want := append(le32(5), le32(0)...)
		want = append(want, le64(123456)...)
		want = append(want, wstr("inv-pass")...)
		want = append(want, wstr("Broker-Live")...)
		f.expect(want)
		f.reply(5, 1)
		f.expect(le32(0x6d))
		f.reply(0x6d, 1, le32(1)) // connecting
		f.expect(le32(0x6d))
		f.reply(0x6d, 1, le32(2)) // authorized
	}()
	if err := c.Login(context.Background(), 123456, "inv-pass", "Broker-Live"); err != nil {
		t.Fatal(err)
	}
}

func TestLoginFailures(t *testing.T) {
	t.Run("bad password", func(t *testing.T) {
		c, f := newTestClient(t)
		go func() {
			f.readRequest()
			f.reply(5, 1)
			f.expect(le32(0x6d))
			f.reply(0x6d, 1, le32(3))
		}()
		if err := c.Login(context.Background(), 1, "x", "s"); !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("otp required", func(t *testing.T) {
		c, f := newTestClient(t)
		go func() {
			f.readRequest()
			f.reply(5, ResUnsupported)
		}()
		if err := c.Login(context.Background(), 1, "x", "s"); err != ErrUnsupportedAuth {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		c, f := newTestClient(t)
		go func() {
			f.readRequest()
			f.reply(5, 1)
			for {
				var hdr [8]byte
				if _, err := io.ReadFull(f.conn, hdr[:]); err != nil {
					return
				}
				f.reply(0x6d, 1, le32(1))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
		defer cancel()
		var e *Error
		if err := c.Login(ctx, 1, "x", "s"); !errors.As(err, &e) || e.Code != ResInternalFailTimeout {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestHistoryDeals(t *testing.T) {
	c, f := newTestClient(t)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	go func() {
		// u32 cmd=0x97 | i64 from | i64 to
		f.expect(append(append(le32(0x97), le64(uint64(from.Unix()))...), le64(uint64(to.Unix()))...))
		f.reply(0x97, 1, le32(3), dealRecord(1, "EURUSD", 12.5), dealRecord(2, "XAUUSD", -3), dealRecord(3, "GBPJPY", 1))
	}()
	deals, err := c.HistoryDeals(context.Background(), from, to, "*USD*,!XAU*")
	if err != nil {
		t.Fatal(err)
	}
	if len(deals) != 1 {
		t.Fatalf("got %d deals, want 1 after group filter: %+v", len(deals), deals)
	}
	d := deals[0]
	want := Deal{
		Ticket: 1, Order: 1001, Time: 1700000000, TimeMsc: 1700000000123,
		Type: DealTypeSell, Entry: DealEntryOut, Magic: 77, PositionID: 555, Reason: DealReasonTP,
		Volume: 0.1, Price: 1.0850, Commission: -0.7, Swap: -0.12, Profit: 12.5,
		Symbol: "EURUSD", Comment: "[tp 1.08500]", ExternalID: "ext-1",
	}
	if d != want {
		t.Fatalf("deal\n got %+v\nwant %+v", d, want)
	}
	if got := d.ServerTime(); !got.Equal(time.UnixMilli(1700000000123)) {
		t.Fatalf("ServerTime = %v", got)
	}
}

func TestHistoryDealsEmptyAndError(t *testing.T) {
	c, f := newTestClient(t)
	go func() {
		f.readRequest()
		f.reply(0x97, 1, le32(0))
		f.readRequest()
		f.reply(0x97, ResInvalidParams)
	}()
	deals, err := c.HistoryDeals(context.Background(), time.Unix(0, 0), time.Now(), "")
	if err != nil || len(deals) != 0 {
		t.Fatalf("deals=%v err=%v", deals, err)
	}
	var e *Error
	if _, err := c.HistoryDeals(context.Background(), time.Unix(0, 0), time.Now(), ""); !errors.As(err, &e) || e.Code != ResInvalidParams {
		t.Fatalf("err = %v", err)
	}
}

func TestHistoryDealByTicketAndPosition(t *testing.T) {
	c, f := newTestClient(t)
	go func() {
		f.expect(append(le32(0x99), le64(42)...))
		f.reply(0x99, 1, dealRecord(42, "EURUSD", 1))
		f.expect(append(le32(0x98), le64(555)...))
		f.reply(0x98, 1, le32(2), dealRecord(1, "EURUSD", 0), dealRecord(2, "EURUSD", 5))
	}()
	d, err := c.HistoryDealByTicket(context.Background(), 42)
	if err != nil || d.Ticket != 42 {
		t.Fatalf("deal=%+v err=%v", d, err)
	}
	deals, err := c.HistoryDealsByPosition(context.Background(), 555)
	if err != nil || len(deals) != 2 || deals[1].Profit != 5 {
		t.Fatalf("deals=%+v err=%v", deals, err)
	}
}

func TestHistoryOrders(t *testing.T) {
	c, f := newTestClient(t)
	go func() {
		f.expect(append(append(le32(0x8d), le64(10)...), le64(20)...))
		f.reply(0x8d, 1, le32(1), orderRecord(9, "EURUSD"))
		f.expect(append(le32(0x8f), le64(9)...))
		f.reply(0x8f, 1, orderRecord(9, "EURUSD"))
	}()
	orders, err := c.HistoryOrders(context.Background(), time.Unix(10, 0), time.Unix(20, 0), "")
	if err != nil || len(orders) != 1 {
		t.Fatalf("orders=%v err=%v", orders, err)
	}
	o := orders[0]
	if o.Ticket != 9 || o.TimeSetupMsc != 1700000000500 || o.Type != OrderTypeBuyLimit ||
		o.State != OrderStateFilled || o.PositionID != 555 || o.Reason != OrderReasonExpert ||
		o.VolumeInitial != 0.5 || o.PriceOpen != 1.07 || o.PriceStopLimit != 1.075 ||
		o.Symbol != "EURUSD" || o.ExternalID != "ext-o" {
		t.Fatalf("order = %+v", o)
	}
	if _, err := c.HistoryOrderByTicket(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
}

func TestPipeNameForPath(t *testing.T) {
	a := PipeNameForPath(`\\?\C:\Program Files\MetaTrader 5\terminal64.exe`)
	b := PipeNameForPath(`\\?\c:\program files\metatrader 5\TERMINAL64.EXE`)
	if a != b {
		t.Fatal("pipe name must be case-insensitive")
	}
	if len(a) != len(PipePrefix)+64 {
		t.Fatalf("bad pipe name %q", a)
	}
}

func TestMatchGroup(t *testing.T) {
	cases := []struct {
		group, sym string
		want       bool
	}{
		{"*", "EURUSD", true},
		{"EUR*", "EURUSD", true},
		{"EUR*", "GBPUSD", false},
		{"*USD*,!EUR*", "EURUSD", false},
		{"!EUR*,*USD*", "EURUSD", false},
		{"*USD*,!EUR*", "GBPUSD", true},
		{" GBP* , XAU*", "XAUUSD", true},
		{"eur*", "EURUSD", false},
		{"*JPY", "GBPJPY", true},
		{"E*U*D", "EURUSD", true},
	}
	for _, tc := range cases {
		if got := matchGroup(tc.group, tc.sym); got != tc.want {
			t.Errorf("matchGroup(%q, %q) = %v, want %v", tc.group, tc.sym, got, tc.want)
		}
	}
}
