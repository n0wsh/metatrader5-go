package webterm

import (
	"context"
	"encoding/hex"
	"fmt"
	"unicode/utf16"
)

// Login authenticates the connection. An investor (read-only) password
// gives full access to the history and to the account state, and any trade
// request is rejected by the server.
func (c *Client) Login(ctx context.Context, opts LoginOptions) (*LoginResult, error) {
	// The terminal sends the password twice: the first 32 characters as
	// text, and, when the password is a 320 character hex blob, the
	// decoded 160 bytes.
	prefix := ""
	blob := make([]byte, 160)
	if len(opts.Password) == 320 {
		decoded, err := hex.DecodeString(opts.Password)
		if err != nil {
			return nil, fmt.Errorf("webterm: password blob: %w", err)
		}
		blob = decoded
	} else {
		prefix = truncateUTF16(opts.Password, 32)
	}

	host := opts.Host
	if host == "" {
		host = c.host
	}
	server := truncateUTF16(host, 128)
	serverLen := len(utf16.Encode([]rune(host)))
	if serverLen > 128 {
		serverLen = 128
	}
	payload := serialize([]value{
		u32(opts.Version),                       // client version
		wchar(prefix, 64),                       // password
		wchar(truncateUTF16(opts.OTP, 64), 128), // one time password
		bytesVal(c.clientID, 16),                // device id
		wchar("", 64),                           // utm campaign
		wchar("", 64),                           // utm source
		u64(0),                                  // referrer cookie id
		wchar("", 128),                          // affiliate site
		u32(uint32(serverLen)),
		wchar(server, 256), // trade server
		u64(opts.Login),
		bytesVal(blob, 160), // password blob
		u64(opts.Session),   // session id
		u64(0),
	})

	resp, err := c.call(ctx, cmdLogin, payload)
	if err != nil {
		return nil, err
	}
	if resp.code != 0 {
		return nil, loginError(resp.code)
	}
	values, err := parse(resp.body, loginResponseSchema, 0)
	if err != nil {
		return nil, err
	}
	token := values[0].([]byte)
	session := values[1].(uint64)

	c.mu.Lock()
	c.token = append([]byte(nil), token...)
	c.session = session
	c.login = opts.Login
	c.loggedIn = true
	c.mu.Unlock()

	return &LoginResult{Token: append([]byte(nil), token...), Session: session}, nil
}

// truncateUTF16 cuts a string to at most n UTF-16 code units, which is how
// the terminal limits its text fields.
func truncateUTF16(s string, n int) string {
	units := utf16.Encode([]rune(s))
	if len(units) <= n {
		return s
	}
	return string(utf16.Decode(units[:n]))
}
