# Position protocol fixtures

`open_positions.bin` is a synthetic, unencrypted command 4 reply, not account
or credential data. It contains one 344-byte position and one 356-byte pending
order (zero-filled). Its count fields are little-endian uint32.

The record layout comes from the captured build 6316 terminal bundle
`D09aEvCB.js` (SHA-256
`a13d74612c82f3009e58daacf21bcbf031d338987086257bd5dad17ba0fb4cd5`). The `Hu` schema defines the fields, `Jt` maps
record slots, and `Ju.ol` requests command 4 and decodes positions followed by
orders. These are static source observations; no new live account validation
was performed. REA's analyzer was unavailable under the host filesystem
restrictions, so the captured source was inspected directly.

The fixture was packed independently with Python `struct` using format
`<qqII64sI4dQ5dqq64sdIII64sii`, with UTF-16LE strings padded to 64 bytes.
It deliberately includes a position ID above 2^53, nonzero seconds and
millisecond halves, a non-ASCII comment, and distinct prices/costs.

| Record byte offset | Field | Value |
|---|---|---|
| 0 | position ID | 9007199254740993 |
| 8 | order | 1234 |
| 16, 20 | create/update seconds | 1700000000, 1700000200 |
| 24 | symbol | EURUSD |
| 88 | direction | 1 (sell) |
| 92, 100, 108, 116 | open/current/SL/TP | 1.125, 1.1, 1.15, 1.05 |
| 124 | volume | 125000000 (1.25 lots) |
| 132, 140, 148, 156, 164 | profit/rates/commission/swap | 3125.5, 1, 2, -3.25, -1.5 |
| 172, 180 | expert/reserved reference | 77, 88 |
| 188 | comment | open 倉 |
| 252 | contract size | 100000 |
| 260, 264, 268 | digits/currency digits/reason | 5, 2, 4 |
| 272 | external ID | external-42 |
| 336, 340 | create/update milliseconds | 123, 987 |

The same captured source's `Ot` maps deal `ta` to slot 8 (`Deal.PriceOpen`).
Its `Gc.Le` history aggregator uses `ta` for both entry and exit execution
prices. `Deal.PriceClose` is not used to calculate historical weighted exits.
ClosedPositions follows position lifetimes and only emits final flat positions;
this is deliberately stricter than the terminal's partially-closed history view.
