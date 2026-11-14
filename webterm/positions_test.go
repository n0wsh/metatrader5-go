package webterm

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func positionFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/open_positions.bin")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPositionsSnapshot(t *testing.T) {
	body := positionFixture(t)
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		cmd, request, err := p.read()
		if err != nil {
			t.Error(err)
			return
		}
		if cmd != cmdPositions || len(request) != 0 {
			t.Errorf("positions request: cmd=%d payload=%x", cmd, request)
		}
		if err := p.reply(cmd, 0, body); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = p.read()
	})
	positions, err := c.Positions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Position{{
		PositionID: 9007199254740993, Order: 1234,
		Time: 1700000000123, TimeUpdate: 1700000200987,
		Symbol: "EURUSD", Type: PositionTypeSell, Volume: 1.25,
		PriceOpen: 1.125, PriceCurrent: 1.1, SL: 1.15, TP: 1.05,
		Profit: 3125.5, Commission: -3.25, Swap: -1.5,
		Expert: 77, Comment: "open 倉", ContractSize: 100000,
		Digits: 5, DigitsCurrency: 2, Reason: 4, ExternalID: "external-42",
	}}
	if !reflect.DeepEqual(positions, want) {
		t.Fatalf("positions = %+v; want %+v", positions, want)
	}
	if positionRecordSize != 344 {
		t.Fatalf("position record size = %d", positionRecordSize)
	}
}

func TestParsePositionsEmptyAndMalformed(t *testing.T) {
	empty, err := parsePositions(make([]byte, 8))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty positions: %v, %v", empty, err)
	}
	fixture := positionFixture(t)
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"missing count", nil},
		{"short count", []byte{1, 0, 0}},
		{"huge count", []byte{255, 255, 255, 255}},
		{"short position", fixture[:4+343]},
		{"missing orders", fixture[:4+344]},
		{"short pending order", fixture[:len(fixture)-1]},
		{"trailing bytes", append(append([]byte(nil), fixture...), 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parsePositions(tc.body); err == nil {
				t.Fatal("malformed reply accepted")
			}
		})
	}
	// Malicious counts must be rejected before any allocation proportional to them.
	hugeOrders := append([]byte(nil), fixture...)
	binary.LittleEndian.PutUint32(hugeOrders[4+344:], ^uint32(0))
	if _, err := parsePositions(hugeOrders); err == nil {
		t.Fatal("huge order count accepted")
	}
}

func TestPositionsStatusError(t *testing.T) {
	c := newTestTerminal(t, time.Second, func(p *terminalPeer) {
		cmd, _, err := p.read()
		if err != nil {
			t.Error(err)
			return
		}
		if err := p.reply(cmd, 3, nil); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = p.read()
	})
	_, err := c.Positions(context.Background())
	var status *StatusError
	if !errors.As(err, &status) || status.Command != cmdPositions || status.Code != 3 {
		t.Fatalf("status error: %v", err)
	}
}

func TestHistoryRejectsCountsBeforeAllocation(t *testing.T) {
	for _, body := range [][]byte{
		{255, 255, 255, 255},
		{0, 0, 0, 0, 255, 255, 255, 255},
	} {
		if _, err := parseHistory(body); err == nil {
			t.Fatal("oversized history count accepted")
		}
	}
}

// The extension follows all positions and orders; it is not record padding.
func TestPositionsXMZeroExtension(t *testing.T) {
	fixture := positionFixture(t)
	one := append(append([]byte(nil), fixture...), make([]byte, 12)...)
	positions, orders, err := parseOpenState(one)
	if err != nil || len(positions) != 1 || len(orders) != 1 || positions[0].Symbol != "EURUSD" {
		t.Fatalf("single position extension: %+v, %v", positions, err)
	}
	two := make([]byte, 4)
	binary.LittleEndian.PutUint32(two, 2)
	two = append(two, fixture[4:348]...)
	two = append(two, fixture[4:348]...)
	two = append(two, fixture[348:]...)
	two = append(two, make([]byte, 24)...)
	positions, orders, err = parseOpenState(two)
	if err != nil || len(positions) != 2 || len(orders) != 1 || positions[1].Symbol != "EURUSD" || positions[1].SL != 1.15 {
		t.Fatalf("multiple positions extension: %+v, %v", positions, err)
	}
	bad := append([]byte(nil), one...)
	bad[len(bad)-1] = 1
	for _, body := range [][]byte{bad, one[:len(one)-1], append(one, 0), make([]byte, 20)} {
		if _, _, err := parseOpenState(body); err == nil {
			t.Fatal("unsupported suffix accepted")
		}
	}
}
