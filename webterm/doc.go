// Package webterm speaks the MetaTrader 5 web terminal protocol.
//
// The browser bundle behind web.metatrader.app exposes trade data over a
// binary WebSocket.
// This package is a client for that protocol, recovered from the bundle the
// terminal itself loads.
//
// Wire format (little-endian throughout):
//
//	websocket message := u32 body_len | u32 version | AES-CBC(ciphertext)
//	ciphertext        := AES-CBC(plaintext) with a zero IV and PKCS#7 padding
//	plaintext         := u8 random | u8 random | u16 command | payload
//
// A response reuses the request header shape and adds one status byte:
//
//	plaintext         := u8 random | u8 random | u16 command | u8 status | body
//
// The status byte is zero on success; the command body follows it.
//
// Records are serialized with the terminal's own field format: a list of
// typed fields, each tagged with a property type and an optional byte length.
// Strings are UTF-16LE and zero padded, buffers are raw bytes zero padded.
package webterm
