package mt5legacy

import "time"

const (
	dealRecordSize  = 300
	orderRecordSize = 340
	recordStrLen    = 32
)

// Deal mirrors the Python TradeDeal named tuple.
//
// Times are broker server time encoded as Unix seconds/milliseconds (the same
// values Python returns), not true UTC.
type Deal struct {
	Ticket     uint64     `json:"ticket"`
	Order      uint64     `json:"order"`
	Time       int64      `json:"time"`
	TimeMsc    int64      `json:"time_msc"`
	Type       DealType   `json:"type"`
	Entry      DealEntry  `json:"entry"`
	Magic      uint64     `json:"magic"`
	PositionID uint64     `json:"position_id"`
	Reason     DealReason `json:"reason"`
	Volume     float64    `json:"volume"`
	Price      float64    `json:"price"`
	Commission float64    `json:"commission"`
	Swap       float64    `json:"swap"`
	Profit     float64    `json:"profit"`
	Fee        float64    `json:"fee"`
	Symbol     string     `json:"symbol"`
	Comment    string     `json:"comment"`
	ExternalID string     `json:"external_id"`
}

// ServerTime returns TimeMsc as a time.Time in the UTC location. The wall
// clock it shows is the broker's server time.
func (d Deal) ServerTime() time.Time { return time.UnixMilli(d.TimeMsc).UTC() }

func decodeDeal(d *decoder) Deal {
	return Deal{
		Ticket:     d.u64(),
		Order:      d.u64(),
		Time:       d.i64(),
		TimeMsc:    d.i64(),
		Type:       DealType(d.u32()),
		Entry:      DealEntry(d.u32()),
		Magic:      d.u64(),
		PositionID: d.u64(),
		Reason:     DealReason(d.u32()),
		Volume:     d.f64(),
		Price:      d.f64(),
		Commission: d.f64(),
		Swap:       d.f64(),
		Profit:     d.f64(),
		Fee:        d.f64(),
		Symbol:     d.wfixed(recordStrLen),
		Comment:    d.wfixed(recordStrLen),
		ExternalID: d.wfixed(recordStrLen),
	}
}

// Order mirrors the Python TradeOrder named tuple.
type Order struct {
	Ticket         uint64      `json:"ticket"`
	TimeSetup      int64       `json:"time_setup"`
	TimeSetupMsc   int64       `json:"time_setup_msc"`
	TimeDone       int64       `json:"time_done"`
	TimeDoneMsc    int64       `json:"time_done_msc"`
	TimeExpiration int64       `json:"time_expiration"`
	Type           OrderType   `json:"type"`
	TypeTime       uint32      `json:"type_time"`
	TypeFilling    uint32      `json:"type_filling"`
	State          OrderState  `json:"state"`
	Magic          uint64      `json:"magic"`
	PositionID     uint64      `json:"position_id"`
	PositionByID   uint64      `json:"position_by_id"`
	Reason         OrderReason `json:"reason"`
	VolumeInitial  float64     `json:"volume_initial"`
	VolumeCurrent  float64     `json:"volume_current"`
	PriceOpen      float64     `json:"price_open"`
	SL             float64     `json:"sl"`
	TP             float64     `json:"tp"`
	PriceCurrent   float64     `json:"price_current"`
	PriceStopLimit float64     `json:"price_stoplimit"`
	Symbol         string      `json:"symbol"`
	Comment        string      `json:"comment"`
	ExternalID     string      `json:"external_id"`
}

func decodeOrder(d *decoder) Order {
	return Order{
		Ticket:         d.u64(),
		TimeSetup:      d.i64(),
		TimeSetupMsc:   d.i64(),
		TimeDone:       d.i64(),
		TimeDoneMsc:    d.i64(),
		TimeExpiration: d.i64(),
		Type:           OrderType(d.u32()),
		TypeTime:       d.u32(),
		TypeFilling:    d.u32(),
		State:          OrderState(d.u32()),
		Magic:          d.u64(),
		PositionID:     d.u64(),
		PositionByID:   d.u64(),
		Reason:         OrderReason(d.u32()),
		VolumeInitial:  d.f64(),
		VolumeCurrent:  d.f64(),
		PriceOpen:      d.f64(),
		SL:             d.f64(),
		TP:             d.f64(),
		PriceCurrent:   d.f64(),
		PriceStopLimit: d.f64(),
		Symbol:         d.wfixed(recordStrLen),
		Comment:        d.wfixed(recordStrLen),
		ExternalID:     d.wfixed(recordStrLen),
	}
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
	DealDividend
	DealDividendFranked
	DealTax
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

// DealReason is ENUM_DEAL_REASON.
type DealReason uint32

const (
	DealReasonClient DealReason = iota
	DealReasonMobile
	DealReasonWeb
	DealReasonExpert
	DealReasonSL
	DealReasonTP
	DealReasonSO
	DealReasonRollover
	DealReasonVMargin
	DealReasonSplit
)

func (r DealReason) String() string {
	return enumName([]string{"client", "mobile", "web", "expert", "sl", "tp", "so", "rollover", "vmargin", "split"}, uint32(r))
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
	return enumName([]string{"buy", "sell", "buy_limit", "sell_limit", "buy_stop", "sell_stop", "buy_stop_limit", "sell_stop_limit", "close_by"}, uint32(t))
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
	return enumName([]string{"started", "placed", "canceled", "partial", "filled", "rejected", "expired", "request_add", "request_modify", "request_cancel"}, uint32(s))
}

// OrderReason is ENUM_ORDER_REASON.
type OrderReason uint32

const (
	OrderReasonClient OrderReason = iota
	OrderReasonMobile
	OrderReasonWeb
	OrderReasonExpert
	OrderReasonSL
	OrderReasonTP
	OrderReasonSO
)

func (r OrderReason) String() string {
	return enumName([]string{"client", "mobile", "web", "expert", "sl", "tp", "so"}, uint32(r))
}

func enumName(names []string, v uint32) string {
	if int(v) < len(names) {
		return names[v]
	}
	return "unknown"
}
