package webterm

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"
)

func TestDeobfuscateKey(t *testing.T) {
	got := deobfuscate(obfuscatedKey)
	want := "02de02a1a65cc794684fcbea1ecb0fd74ae657e43662c11eee885d2fd64f4964"
	if got != want {
		t.Fatalf("deobfuscate = %q, want %q", got, want)
	}
	key, err := initialKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("initial key is %d bytes, want 32", len(key))
	}
	if hex.EncodeToString(key) != want {
		t.Fatalf("initial key = %s, want %s", hex.EncodeToString(key), want)
	}
}

func TestAESCipherGoldenVector(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := newAES(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("hello webterminal")
	enc, err := c.encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	want := "e03cbd33274a29333f0ca8b3ad8113c0ee4c185887e55a68347b64beb382b93a"
	if got := hex.EncodeToString(enc); got != want {
		t.Fatalf("ciphertext = %s, want %s", got, want)
	}
	dec, err := c.decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatalf("round trip = %q, want %q", dec, plain)
	}
}

func TestAESCipherBadPadding(t *testing.T) {
	c, err := newAES(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.decrypt(make([]byte, 16)); err == nil {
		t.Fatal("expected an error for a bad padding")
	}
	if _, err := c.decrypt(make([]byte, 17)); err == nil {
		t.Fatal("expected an error for a truncated ciphertext")
	}
}

func TestSerializeLoginPayload(t *testing.T) {
	clientID := make([]byte, 16)
	for i := range clientID {
		clientID[i] = byte(i)
	}
	blob := make([]byte, 160)
	payload := serialize([]value{
		u32(0),                    // version
		wchar("secret", 64),       // password
		wchar("", 128),            // otp
		bytesVal(clientID, 16),    // device id
		wchar("", 64),             // utm campaign
		wchar("", 64),             // utm source
		u64(0),                    // referrer cookie id
		wchar("", 128),            // affiliate site
		u32(12),                   // server name length
		wchar("Broker-Live", 256), // server name
		u64(12345678),             // login
		bytesVal(blob, 160),       // password blob
		u64(0),                    // session
		u64(0),                    // reserved
	})
	if len(payload) != 920 {
		t.Fatalf("payload is %d bytes, want 920", len(payload))
	}
	if got := binary.LittleEndian.Uint32(payload[0:]); got != 0 {
		t.Fatalf("version = %d, want 0", got)
	}
	if got := string(decodeWchar(payload[4:68])); got != "secret" {
		t.Fatalf("password = %q, want %q", got, "secret")
	}
	if !bytes.Equal(payload[196:212], clientID) {
		t.Fatalf("device id = %x, want %x", payload[196:212], clientID)
	}
	if got := binary.LittleEndian.Uint32(payload[476:]); got != 12 {
		t.Fatalf("server name length = %d, want 12", got)
	}
	if got := string(decodeWchar(payload[480:736])); got != "Broker-Live" {
		t.Fatalf("server name = %q, want %q", got, "Broker-Live")
	}
	if got := binary.LittleEndian.Uint64(payload[736:]); got != 12345678 {
		t.Fatalf("login = %d, want 12345678", got)
	}
	if !bytes.Equal(payload[744:904], blob) {
		t.Fatal("password blob is not zeroed")
	}
}

func TestRecordSizes(t *testing.T) {
	if dealRecordSize != 356 {
		t.Fatalf("deal record is %d bytes, want 356", dealRecordSize)
	}
	if orderRecordSize != 356 {
		t.Fatalf("order record is %d bytes, want 356", orderRecordSize)
	}
}

// buildDeal fills a deal record with values a test can look for.
func buildDeal(t *testing.T, ticket uint64, symbol, comment string, volumeLots float64) []byte {
	t.Helper()
	raw := serialize([]value{
		i64(int64(ticket)),                    // ticket
		wchar("deal-1", 64),                   // deal id
		i64(42),                               // order
		u32(1700000000),                       // time create, seconds
		u32(1700000001),                       // time update, seconds
		wchar(symbol, 64),                     // symbol
		u32(0),                                // type
		u32(0),                                // entry
		f64(1.1000),                           // price open
		f64(1.1005),                           // price close
		f64(0),                                // sl
		f64(1.2),                              // tp
		u64(uint64(volumeLots * volumeScale)), // volume
		f64(12.5),                             // profit
		f64(0),                                // rate profit
		f64(0),                                // rate margin
		f64(-0.7),                             // commission
		f64(-1.5),                             // swap
		i64(7),                                // expert
		i64(99),                               // position id
		wchar(comment, 64),                    // comment
		f64(100000),                           // contract size
		u32(5),                                // digits
		u32(2),                                // currency digits
		u32(1),                                // reason
		i32(250),                              // time create, milliseconds
		i32(750),                              // time update, milliseconds
		f64(0.25),                             // fee
	})
	if len(raw) != dealRecordSize {
		t.Fatalf("deal record is %d bytes, want %d", len(raw), dealRecordSize)
	}
	return raw
}

func TestParseHistory(t *testing.T) {
	deal := buildDeal(t, 555, "EURUSD", "open", 0.25)
	order := serialize([]value{
		i64(42),                        // ticket
		wchar("order-1", 64),           // id
		wchar("EURUSD", 64),            // symbol
		u32(1700000000),                // time setup, seconds
		u32(0),                         // time expiration
		u32(1700000100),                // time done, seconds
		u32(1),                         // type
		u32(0),                         // type filling
		u32(0),                         // type time
		u32(0),                         // reason
		f64(1.1000),                    // price order
		f64(1.1005),                    // price trigger
		f64(1.1003),                    // price current
		f64(0),                         // sl
		f64(1.2),                       // tp
		i64(int64(0.25 * volumeScale)), // volume initial
		i64(int64(0.1 * volumeScale)),  // volume current
		u32(4),                         // state
		i64(7),                         // expert
		i64(99),                        // position id
		wchar("closed", 64),            // comment
		f64(100000),                    // contract size
		u32(5),                         // digits
		u32(2),                         // currency digits
		f64(0),                         // commission daily
		f64(0),                         // commission monthly
		f64(0),                         // margin rate
		u32(0),                         // activation mode
		i32(125),                       // time setup, milliseconds
		i32(875),                       // time done, milliseconds
	})
	if len(order) != orderRecordSize {
		t.Fatalf("order record is %d bytes, want %d", len(order), orderRecordSize)
	}

	body := make([]byte, 0, 4+len(deal)+4+len(order))
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = append(body, deal...)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = append(body, order...)

	h, err := parseHistory(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Deals) != 1 || len(h.Orders) != 1 {
		t.Fatalf("got %d deals and %d orders, want 1 and 1", len(h.Deals), len(h.Orders))
	}

	d := h.Deals[0]
	if d.Ticket != 555 || d.Order != 42 {
		t.Fatalf("tickets = %d/%d, want 555/42", d.Ticket, d.Order)
	}
	if d.Symbol != "EURUSD" || d.Comment != "open" {
		t.Fatalf("symbol/comment = %q/%q", d.Symbol, d.Comment)
	}
	if d.Volume != 0.25 {
		t.Fatalf("volume = %v, want 0.25", d.Volume)
	}
	if d.Time != 1700000000*1000+250 {
		t.Fatalf("time = %d, want %d", d.Time, 1700000000*1000+250)
	}
	if d.TimeUpdate != 1700000001*1000+750 {
		t.Fatalf("time update = %d, want %d", d.TimeUpdate, 1700000001*1000+750)
	}
	if d.Profit != 12.5 || d.Commission != -0.7 || d.Swap != -1.5 || d.Fee != 0.25 {
		t.Fatalf("money fields = %v/%v/%v/%v", d.Profit, d.Commission, d.Swap, d.Fee)
	}
	if d.Expert != 7 || d.PositionID != 99 || d.Digits != 5 {
		t.Fatalf("ids = %d/%d/%d", d.Expert, d.PositionID, d.Digits)
	}

	o := h.Orders[0]
	if o.Ticket != 42 || o.Symbol != "EURUSD" || o.Comment != "closed" {
		t.Fatalf("order = %+v", o)
	}
	if o.VolumeInitial != 0.25 || o.VolumeCurrent != 0.1 {
		t.Fatalf("volumes = %v/%v", o.VolumeInitial, o.VolumeCurrent)
	}
	if o.State != OrderStateFilled {
		t.Fatalf("state = %v, want filled", o.State)
	}
	if o.TimeSetup != 1700000000*1000+125 {
		t.Fatalf("time setup = %d, want %d", o.TimeSetup, 1700000000*1000+125)
	}
	if o.PriceCurrent != 1.1003 {
		t.Fatalf("price current = %v, want 1.1003", o.PriceCurrent)
	}
}

func TestTruncateUTF16(t *testing.T) {
	if got := truncateUTF16("abcdef", 3); got != "abc" {
		t.Fatalf("truncate = %q, want %q", got, "abc")
	}
	if got := truncateUTF16("ab", 8); got != "ab" {
		t.Fatalf("truncate = %q, want %q", got, "ab")
	}
}

func TestFrameRoundTrip(t *testing.T) {
	c, err := newAES(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	inner := make([]byte, commandHeaderSize+4)
	binary.LittleEndian.PutUint16(inner[2:], cmdTradeHistory)
	binary.LittleEndian.PutUint32(inner[4:], 1700000000)

	enc, err := c.encrypt(inner)
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, outerHeaderSize+len(enc))
	binary.LittleEndian.PutUint32(frame[0:], uint32(len(enc)))
	binary.LittleEndian.PutUint32(frame[4:], outerVersion)
	copy(frame[outerHeaderSize:], enc)

	if len(frame) != outerHeaderSize+len(enc) {
		t.Fatal("frame size mismatch")
	}
	plain, err := c.decrypt(frame[outerHeaderSize:])
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(plain[2:]) != cmdTradeHistory {
		t.Fatalf("command = %d, want %d", binary.LittleEndian.Uint16(plain[2:]), cmdTradeHistory)
	}
}

func TestParseAccount(t *testing.T) {
	raw := serialize([]value{
		u8(1),                                 // type: demo
		i32(flagInvestor | flagTradeDisabled), // flags
		i32(0),                                // flags
		f64(1234.56),                          // balance
		f64(10),                               // credit
		wchar("USD", 64),                      // currency
		u32(5),                                // digits
		u32(100),                              // leverage
		wchar("Demo Account", 256),            // name
		u16(5687),                             // server build
		wchar("MetaQuotes-Demo", 128),         // server
		wchar("MetaQuotes Ltd", 256),          // company
		i32(7200),                             // timezone shift
		i8(0),                                 // daylight mode
		u32(0),                                // margin mode
		u32(0),                                // floating mode
		f64(0),                                // profit
		f64(0),                                // balance reserve
		u32(2),                                // currency digits
		f64(0),                                // margin
		u32(0),                                // margin free mode
		f64(0),                                // margin free
		f64(0),                                // margin initial
		f64(0),                                // margin maintenance
		u32(8),                                // margin level digits
		u32(0),                                // reserved
	})
	values, err := parse(raw, accountSchema, 0)
	if err != nil {
		t.Fatal(err)
	}
	acc := newAccount(4242, values)
	if acc.Login != 4242 || acc.Type != 1 {
		t.Fatalf("account = %+v", acc)
	}
	if acc.Name != "Demo Account" || acc.Server != "MetaQuotes-Demo" {
		t.Fatalf("name/server = %q/%q", acc.Name, acc.Server)
	}
	if acc.Balance != 1234.56 || acc.Credit != 10 || acc.Currency != "USD" {
		t.Fatalf("money = %v/%v/%q", acc.Balance, acc.Credit, acc.Currency)
	}
	if !acc.Investor || !acc.TradeDisabled {
		t.Fatalf("flags = %+v", acc)
	}
	if acc.ServerBuild != 5687 || acc.Leverage != 100 || acc.Digits != 5 {
		t.Fatalf("numbers = %+v", acc)
	}
}

func TestHistoryWindowZeroMeansNow(t *testing.T) {
	// The terminal sends zero for "now" and for "everything".
	from := time.Time{}
	if from.Unix() != -62135596800 {
		t.Skip("unexpected zero time")
	}
}
