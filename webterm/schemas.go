package webterm

// Record sizes, derived from the schemas so they cannot drift apart.
var (
	positionRecordSize = schemaSize(positionSchema)
	dealRecordSize     = schemaSize(dealSchema)
	orderRecordSize    = schemaSize(orderSchema)
)

// The record layouts below are the ones the web terminal uses for the
// trade history (command 5) and the account summary (command 3). Field
// comments give the meaning of every slot.

// dealSchema is one deal record: 356 bytes.
var dealSchema = []field{
	{typ: propInt64},             // 0  ticket
	{typ: propWchar, length: 64}, // 1  deal id
	{typ: propInt64},             // 2  order ticket
	{typ: propUint32},            // 3  time create, seconds
	{typ: propUint32},            // 4  time update, seconds
	{typ: propWchar, length: 64}, // 5  symbol
	{typ: propUint32},            // 6  type
	{typ: propUint32},            // 7  entry
	{typ: propFloat64},           // 8  price open
	{typ: propFloat64},           // 9  price close
	{typ: propFloat64},           // 10 stop loss
	{typ: propFloat64},           // 11 take profit
	{typ: propUint64},            // 12 volume, lots * 1e8
	{typ: propFloat64},           // 13 profit
	{typ: propFloat64},           // 14 rate profit
	{typ: propFloat64},           // 15 rate margin
	{typ: propFloat64},           // 16 commission
	{typ: propFloat64},           // 17 storage (swap)
	{typ: propInt64},             // 18 expert (magic number)
	{typ: propInt64},             // 19 position id
	{typ: propWchar, length: 64}, // 20 comment
	{typ: propFloat64},           // 21 contract size
	{typ: propUint32},            // 22 digits
	{typ: propUint32},            // 23 currency digits
	{typ: propUint32},            // 24 reason
	{typ: propInt32},             // 25 time create, milliseconds
	{typ: propInt32},             // 26 time update, milliseconds
	{typ: propFloat64},           // 27 commission fee
}

// orderSchema is one order record: 356 bytes.
var orderSchema = []field{
	{typ: propInt64},             // 0  ticket
	{typ: propWchar, length: 64}, // 1  order id
	{typ: propWchar, length: 64}, // 2  symbol
	{typ: propUint32},            // 3  time setup, seconds
	{typ: propUint32},            // 4  time expiration, seconds
	{typ: propUint32},            // 5  time done, seconds
	{typ: propUint32},            // 6  type
	{typ: propUint32},            // 7  type filling
	{typ: propUint32},            // 8  type time
	{typ: propUint32},            // 9  reason
	{typ: propFloat64},           // 10 price order
	{typ: propFloat64},           // 11 price trigger
	{typ: propFloat64},           // 12 price current
	{typ: propFloat64},           // 13 stop loss
	{typ: propFloat64},           // 14 take profit
	{typ: propInt64},             // 15 volume initial, lots * 1e8
	{typ: propInt64},             // 16 volume current, lots * 1e8
	{typ: propUint32},            // 17 state
	{typ: propInt64},             // 18 expert (magic number)
	{typ: propInt64},             // 19 position id
	{typ: propWchar, length: 64}, // 20 comment
	{typ: propFloat64},           // 21 contract size
	{typ: propUint32},            // 22 digits
	{typ: propUint32},            // 23 currency digits
	{typ: propFloat64},           // 24 commission daily
	{typ: propFloat64},           // 25 commission monthly
	{typ: propFloat64},           // 26 margin rate
	{typ: propUint32},            // 27 activation mode
	{typ: propInt32},             // 28 time setup, milliseconds
	{typ: propInt32},             // 29 time done, milliseconds
}

// accountSchema is the account summary that follows the account command:
// 26 fields.
var accountSchema = []field{
	{typ: propUint8},              // 0  account type
	{typ: propInt32},              // 1  permission flags
	{typ: propInt32},              // 2  permission flags
	{typ: propFloat64},            // 3  balance
	{typ: propFloat64},            // 4  credit
	{typ: propWchar, length: 64},  // 5  currency
	{typ: propUint32},             // 6  digits
	{typ: propUint32},             // 7  leverage
	{typ: propWchar, length: 256}, // 8  account name
	{typ: propUint16},             // 9  server build
	{typ: propWchar, length: 128}, // 10 server name
	{typ: propWchar, length: 256}, // 11 company
	{typ: propInt32},              // 12 timezone shift, seconds
	{typ: propInt8},               // 13 daylight saving mode
	{typ: propUint32},             // 14 margin mode
	{typ: propUint32},             // 15 floating mode
	{typ: propFloat64},            // 16 profit
	{typ: propFloat64},            // 17 balance reserve
	{typ: propUint32},             // 18 currency digits
	{typ: propFloat64},            // 19 margin
	{typ: propUint32},             // 20 margin free mode
	{typ: propFloat64},            // 21 margin free
	{typ: propFloat64},            // 22 margin initial
	{typ: propFloat64},            // 23 margin maintenance
	{typ: propUint32},             // 24 margin level digits
	{typ: propUint32},             // 25 reserved
}

// loginResponseSchema is the reply to the login command: a 160 byte token
// and the session id.
var loginResponseSchema = []field{
	{typ: propBuffer, length: 160},
	{typ: propUint64},
}

// positionSchema is command 4's 344-byte open-position record. It follows
// build 6316's Hu schema and Jt field mapping; see testdata/README.md.
var positionSchema = []field{
	{typ: propInt64},             // 0 position id
	{typ: propInt64},             // 1 order
	{typ: propUint32},            // 2 time create, seconds
	{typ: propUint32},            // 3 time update, seconds
	{typ: propWchar, length: 64}, // 4 symbol
	{typ: propUint32},            // 5 direction
	{typ: propFloat64},           // 6 open price
	{typ: propFloat64},           // 7 current price
	{typ: propFloat64},           // 8 stop loss
	{typ: propFloat64},           // 9 take profit
	{typ: propUint64},            // 10 volume, lots * 1e8
	{typ: propFloat64},           // 11 profit
	{typ: propFloat64},           // 12 profit conversion rate
	{typ: propFloat64},           // 13 margin conversion rate
	{typ: propFloat64},           // 14 commission
	{typ: propFloat64},           // 15 swap
	{typ: propInt64},             // 16 expert
	{typ: propInt64},             // 17 reserved position reference
	{typ: propWchar, length: 64}, // 18 comment
	{typ: propFloat64},           // 19 contract size
	{typ: propUint32},            // 20 price digits
	{typ: propUint32},            // 21 currency digits
	{typ: propUint32},            // 22 reason
	{typ: propWchar, length: 64}, // 23 external id
	{typ: propInt32},             // 24 time create, milliseconds
	{typ: propInt32},             // 25 time update, milliseconds
}
