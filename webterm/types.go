package webterm

import (
	"encoding/binary"
	"fmt"
)

// volumeScale is how the terminal encodes volumes: the wire value is the
// number of lots multiplied by 1e8.
const volumeScale = 1e8

// Deal is one entry of the trade history.
type Deal struct {
	Ticket         uint64    `json:"ticket"`
	Order          uint64    `json:"order"`
	Time           int64     `json:"time"`        // unix milliseconds
	TimeUpdate     int64     `json:"time_update"` // unix milliseconds
	Type           DealType  `json:"type"`
	Entry          DealEntry `json:"entry"`
	Expert         uint64    `json:"expert"`
	PositionID     uint64    `json:"position_id"`
	Reason         uint32    `json:"reason"`
	Volume         float64   `json:"volume"`
	PriceOpen      float64   `json:"price_open"`
	PriceClose     float64   `json:"price_close"`
	SL             float64   `json:"sl"`
	TP             float64   `json:"tp"`
	Profit         float64   `json:"profit"`
	Commission     float64   `json:"commission"`
	Swap           float64   `json:"swap"`
	Fee            float64   `json:"fee"`
	Symbol         string    `json:"symbol"`
	Comment        string    `json:"comment"`
	DealID         string    `json:"deal_id"`
	ContractSize   float64   `json:"contract_size"`
	Digits         uint32    `json:"digits"`
	DigitsCurrency uint32    `json:"digits_currency"`
}

// Order is one entry of the order history.
type Order struct {
	Ticket         uint64     `json:"ticket"`
	TimeSetup      int64      `json:"time_setup"`      // unix milliseconds
	TimeSetupMsc   int64      `json:"time_setup_msc"`  // unix milliseconds
	TimeExpiration int64      `json:"time_expiration"` // unix milliseconds
	TimeDone       int64      `json:"time_done"`       // unix milliseconds
	TimeDoneMsc    int64      `json:"time_done_msc"`   // unix milliseconds
	Type           OrderType  `json:"type"`
	TypeFilling    uint32     `json:"type_filling"`
	TypeTime       uint32     `json:"type_time"`
	Reason         uint32     `json:"reason"`
	VolumeInitial  float64    `json:"volume_initial"`
	VolumeCurrent  float64    `json:"volume_current"`
	PriceOrder     float64    `json:"price_order"`
	PriceTrigger   float64    `json:"price_trigger"`
	PriceCurrent   float64    `json:"price_current"`
	SL             float64    `json:"sl"`
	TP             float64    `json:"tp"`
	State          OrderState `json:"state"`
	Expert         uint64     `json:"expert"`
	PositionID     uint64     `json:"position_id"`
	Symbol         string     `json:"symbol"`
	Comment        string     `json:"comment"`
	OrderID        string     `json:"order_id"`
	ContractSize   float64    `json:"contract_size"`
	Digits         uint32     `json:"digits"`
	DigitsCurrency uint32     `json:"digits_currency"`
}

// Account is the account summary the terminal sends for the account
// command.
type Account struct {
	Login         uint64  `json:"login"`
	Type          uint8   `json:"type"`
	Name          string  `json:"name"`
	Company       string  `json:"company"`
	Server        string  `json:"server"`
	ServerBuild   uint16  `json:"server_build"`
	Currency      string  `json:"currency"`
	Digits        uint32  `json:"digits"`
	Leverage      uint32  `json:"leverage"`
	Balance       float64 `json:"balance"`
	Credit        float64 `json:"credit"`
	TimezoneShift int32   `json:"timezone_shift"`
	DaylightMode  int8    `json:"daylight_mode"`
	// Investor is set when the account was opened with the read-only
	// password.
	Investor bool `json:"investor"`
	// TradeDisabled is set when the account may not place trades.
	TradeDisabled bool `json:"trade_disabled"`
}

// Permission bits of the account flags field.
const (
	flagCanChangePassword = 2
	flagTradeDisabled     = 4
	flagInvestor          = 8
	flagReadOnly          = 512
)

func newDeal(v []any) Deal {
	d := Deal{
		Ticket:         uint64(v[0].(int64)),
		DealID:         v[1].(string),
		Order:          uint64(v[2].(int64)),
		Time:           dealTime(v[3].(uint32), v[25].(int32)),
		TimeUpdate:     dealTime(v[4].(uint32), v[26].(int32)),
		Symbol:         v[5].(string),
		Type:           DealType(v[6].(uint32)),
		Entry:          DealEntry(v[7].(uint32)),
		PriceOpen:      v[8].(float64),
		PriceClose:     v[9].(float64),
		SL:             v[10].(float64),
		TP:             v[11].(float64),
		Volume:         float64(v[12].(uint64)) / volumeScale,
		Profit:         v[13].(float64),
		Commission:     v[16].(float64),
		Swap:           v[17].(float64),
		Expert:         uint64(v[18].(int64)),
		PositionID:     uint64(v[19].(int64)),
		Comment:        v[20].(string),
		ContractSize:   v[21].(float64),
		Digits:         v[22].(uint32),
		DigitsCurrency: v[23].(uint32),
		Reason:         v[24].(uint32),
		Fee:            v[27].(float64),
	}
	return d
}

func newOrder(v []any) Order {
	return Order{
		Ticket:         uint64(v[0].(int64)),
		OrderID:        v[1].(string),
		Symbol:         v[2].(string),
		TimeSetup:      dealTime(v[3].(uint32), v[28].(int32)),
		TimeSetupMsc:   int64(v[28].(int32)),
		TimeExpiration: int64(v[4].(uint32)) * 1000,
		TimeDone:       dealTime(v[5].(uint32), v[29].(int32)),
		TimeDoneMsc:    int64(v[29].(int32)),
		Type:           OrderType(v[6].(uint32)),
		TypeFilling:    v[7].(uint32),
		TypeTime:       v[8].(uint32),
		Reason:         v[9].(uint32),
		PriceOrder:     v[10].(float64),
		PriceTrigger:   v[11].(float64),
		PriceCurrent:   v[12].(float64),
		SL:             v[13].(float64),
		TP:             v[14].(float64),
		VolumeInitial:  float64(v[15].(int64)) / volumeScale,
		VolumeCurrent:  float64(v[16].(int64)) / volumeScale,
		State:          OrderState(v[17].(uint32)),
		Expert:         uint64(v[18].(int64)),
		PositionID:     uint64(v[19].(int64)),
		Comment:        v[20].(string),
		ContractSize:   v[21].(float64),
		Digits:         v[22].(uint32),
		DigitsCurrency: v[23].(uint32),
	}
}

func newAccount(login uint64, v []any) Account {
	flags := v[1].(int32)
	return Account{
		Login:         login,
		Type:          v[0].(uint8),
		Balance:       v[3].(float64),
		Credit:        v[4].(float64),
		Currency:      v[5].(string),
		Digits:        v[6].(uint32),
		Leverage:      v[7].(uint32),
		Name:          v[8].(string),
		ServerBuild:   v[9].(uint16),
		Server:        v[10].(string),
		Company:       v[11].(string),
		TimezoneShift: v[12].(int32),
		DaylightMode:  v[13].(int8),
		Investor:      flags&flagInvestor != 0,
		TradeDisabled: flags&flagTradeDisabled != 0,
	}
}

// dealTime combines the seconds and milliseconds halves of a timestamp.
func dealTime(sec uint32, msec int32) int64 {
	return int64(sec)*1000 + int64(msec)
}

// parseHistory splits the trade history reply into its deal and order
// sections.
func parseHistory(body []byte) (*History, error) {
	h := &History{}
	pos := 0
	dealCount, err := readRecordCount(body, pos, dealRecordSize)
	if err != nil {
		return nil, err
	}
	pos += 4
	h.Deals = make([]Deal, 0, dealCount)
	for i := 0; i < dealCount; i++ {
		values, err := parse(body, dealSchema, pos)
		if err != nil {
			return nil, err
		}
		pos += dealRecordSize
		h.Deals = append(h.Deals, newDeal(values))
	}
	orderCount, err := readRecordCount(body, pos, orderRecordSize)
	if err != nil {
		return nil, err
	}
	pos += 4
	h.Orders = make([]Order, 0, orderCount)
	for i := 0; i < orderCount; i++ {
		values, err := parse(body, orderSchema, pos)
		if err != nil {
			return nil, err
		}
		pos += orderRecordSize
		h.Orders = append(h.Orders, newOrder(values))
	}
	return h, nil
}

// readRecordCount bounds a wire count by the bytes present before it can be
// used for allocation or offset arithmetic (including on 32-bit platforms).
func readRecordCount(body []byte, pos, recordSize int) (int, error) {
	if pos < 0 || pos > len(body) || len(body)-pos < 4 {
		return 0, fmt.Errorf("webterm: truncated record count")
	}
	count := binary.LittleEndian.Uint32(body[pos:])
	if uint64(count) > uint64((len(body)-pos-4)/recordSize) {
		return 0, fmt.Errorf("webterm: record count %d exceeds reply size", count)
	}
	return int(count), nil
}

// DealType is ENUM_DEAL_TYPE.
type DealType uint32

const (
	DealTypeBuy DealType = iota
	DealTypeSell
	DealTypeBalance
	DealTypeCredit
	DealTypeCharge
	DealTypeCorrection
	DealTypeBonus
	DealTypeCommission
	DealTypeCommissionDaily
	DealTypeCommissionMonthly
	DealTypeCommissionAgentDaily
	DealTypeCommissionAgentMonthly
	DealTypeInterest
	DealTypeBuyCanceled
	DealTypeSellCanceled
	DealTypeDividend
	DealTypeDividendFranked
	DealTypeTax
)

var dealTypeNames = []string{
	"buy", "sell", "balance", "credit", "charge", "correction", "bonus",
	"commission", "commission_daily", "commission_monthly",
	"commission_agent_daily", "commission_agent_monthly", "interest",
	"buy_canceled", "sell_canceled", "dividend", "dividend_franked", "tax",
}

func (t DealType) String() string { return enumName(dealTypeNames, uint32(t)) }

// DealEntry is ENUM_DEAL_ENTRY.
type DealEntry uint32

const (
	DealEntryIn DealEntry = iota
	DealEntryOut
	DealEntryInOut
	DealEntryOutBy
)

func (e DealEntry) String() string {
	return enumName([]string{"in", "out", "inout", "out_by"}, uint32(e))
}

// OrderType is ENUM_ORDER_TYPE.
type OrderType uint32

const (
	OrderTypeBuy OrderType = iota
	OrderTypeSell
	OrderTypeBuyLimit
	OrderTypeSellLimit
	OrderTypeBuyStop
	OrderTypeSellStop
	OrderTypeBuyStopLimit
	OrderTypeSellStopLimit
	OrderTypeCloseBy
)

func (t OrderType) String() string {
	return enumName([]string{
		"buy", "sell", "buy_limit", "sell_limit", "buy_stop", "sell_stop",
		"buy_stop_limit", "sell_stop_limit", "close_by",
	}, uint32(t))
}

// OrderState is ENUM_ORDER_STATE.
type OrderState uint32

const (
	OrderStateStarted OrderState = iota
	OrderStatePlaced
	OrderStateCanceled
	OrderStatePartial
	OrderStateFilled
	OrderStateRejected
	OrderStateExpired
	OrderStateRequestAdd
	OrderStateRequestModify
	OrderStateRequestCancel
)

func (s OrderState) String() string {
	return enumName([]string{
		"started", "placed", "canceled", "partial", "filled", "rejected",
		"expired", "request_add", "request_modify", "request_cancel",
	}, uint32(s))
}

func enumName(names []string, v uint32) string {
	if int(v) < len(names) {
		return names[v]
	}
	return "unknown"
}
