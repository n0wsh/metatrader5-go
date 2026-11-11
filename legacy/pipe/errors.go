package mt5legacy

import (
	"errors"
	"fmt"
)

// Result codes, matching MetaTrader5.last_error() in the Python package.
const (
	ResOK                  = 1
	ResFail                = -1
	ResInvalidParams       = -2
	ResNoMemory            = -3
	ResNotFound            = -4
	ResInvalidVersion      = -5
	ResAuthFailed          = -6
	ResUnsupported         = -7
	ResAutoTradingDisabled = -8
	ResInternalFail        = -10000
	ResInternalFailSend    = -10001
	ResInternalFailReceive = -10002
	ResInternalFailInit    = -10003
	ResInternalFailConnect = -10004
	ResInternalFailTimeout = -10005
)

var resMessages = map[int32]string{
	ResFail:                "Terminal: Call failed",
	ResInvalidParams:       "Terminal: Invalid params",
	ResNoMemory:            "Terminal: Out of memory",
	ResNotFound:            "Terminal: Not found",
	ResInvalidVersion:      "Terminal: Incompatible versions, please install the latest version of Terminal",
	ResAuthFailed:          "Terminal: Authorization failed",
	ResUnsupported:         "Terminal: Unsupported function",
	ResAutoTradingDisabled: "Auto-Trading using API disabled in Terminal",
	ResInternalFail:        "Internal error",
	ResInternalFailSend:    "IPC send failed",
	ResInternalFailReceive: "IPC recv failed",
	ResInternalFailInit:    "IPC initialize failed",
	ResInternalFailConnect: "No IPC connection",
	ResInternalFailTimeout: "IPC timeout",
}

// Error is a terminal result code.
type Error struct {
	Code    int32
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("mt5: %s (%d)", e.Message, e.Code) }

func resError(code int32) *Error {
	msg, ok := resMessages[code]
	if !ok {
		msg = "Unknown error"
	}
	return &Error{Code: code, Message: msg}
}

// ErrAuthFailed is returned by Login when the terminal rejects the credentials.
var ErrAuthFailed = &Error{Code: ResAuthFailed, Message: resMessages[ResAuthFailed]}

// ErrUnsupportedAuth is returned by Login when the account requires OTP or a
// certificate, which the API cannot provide.
var ErrUnsupportedAuth = &Error{Code: ResUnsupported, Message: "Unsupported authorization mode, OTP or certificate password needed"}

// Is lets errors.Is match on result code.
func (e *Error) Is(target error) bool {
	var t *Error
	return errors.As(target, &t) && t.Code == e.Code
}
