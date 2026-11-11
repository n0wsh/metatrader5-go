package webterm

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultURL is MetaQuotes' public web terminal. Brokers that run their own
// web terminal serve the same protocol on their own host.
const DefaultURL = "wss://web.metatrader.app/terminal"

// DefaultTimeout bounds the total time for a command, including queueing,
// writing and waiting for its reply.
const DefaultTimeout = 30 * time.Second

// Options configures a connection.
type Options struct {
	// URL is the web terminal endpoint.
	URL string
	// ClientID is the 16 byte device id sent with the login request.
	// A random one is used when empty.
	ClientID []byte
	// Token is a session token from an earlier login. Passing it lets the
	// server resume that session instead of starting a new one.
	Token []byte
	// Timeout bounds the total time for a command, including queueing,
	// writing and waiting for its reply.
	Timeout time.Duration
	// Header carries extra HTTP headers for the WebSocket handshake.
	Header http.Header
}

// LoginOptions describes the account to log into.
type LoginOptions struct {
	// Login is the account number.
	Login uint64
	// Password is the account password. The investor (read-only)
	// password gives access to the history and to the account state,
	// but not to trading.
	Password string
	// Host is the web terminal host the session comes from, for
	// example "web.metatrader.app" or the broker's web terminal
	// domain. The browser sends the host of the page that linked to
	// the terminal, so it is a hint and not the trade server name.
	// The host of the connection is used when it is empty.
	Host string
	// OTP is the one time password, when the account requires one.
	OTP string
	// Version is the terminal version the client announces.
	Version uint32
	// Session is a session id from an earlier login.
	Session uint64
}

// LoginResult is what the server returns after a successful login.
type LoginResult struct {
	// Token identifies the session, it can be passed back in Options.
	Token []byte
	// Session is the numeric session id.
	Session uint64
}

// History is a snapshot of the trade history.
type History struct {
	Deals  []Deal  `json:"deals"`
	Orders []Order `json:"orders"`
}

// Client is a connection to a web terminal. Commands are serialized, so callers
// may use it concurrently. Login and Logout must not overlap other operations.
// If a command is canceled or times out after it starts, the connection is
// closed to prevent a late reply from being delivered to a later command.
// Obtain clients with Dial; the zero value is not usable.
type Client struct {
	conn   *websocket.Conn
	cipher *aesCipher

	mu      sync.Mutex
	pending *pendingRequest
	gate    chan struct{}
	done    chan struct{}
	err     error

	build    uint16
	host     string
	clientID []byte
	token    []byte
	session  uint64
	login    uint64
	loggedIn bool
	timeout  time.Duration
}

// Dial connects to a web terminal and completes the bootstrap handshake.
// No credentials are needed for that; the server hands out a session key
// that all later traffic is encrypted with.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	dialer := *websocket.DefaultDialer
	return dial(ctx, opts, &dialer)
}

func dial(ctx context.Context, opts Options, dialer *websocket.Dialer) (*Client, error) {
	url := opts.URL
	if url == "" {
		url = DefaultURL
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	clientID := opts.ClientID
	if len(clientID) == 0 {
		clientID = make([]byte, 16)
		if _, err := rand.Read(clientID); err != nil {
			return nil, err
		}
	}
	if len(clientID) != 16 {
		return nil, fmt.Errorf("webterm: client id must be 16 bytes, got %d", len(clientID))
	}

	conn, _, err := dialer.DialContext(ctx, url, opts.Header)
	if err != nil {
		return nil, fmt.Errorf("webterm: connect %s: %w", url, err)
	}

	key, err := initialKey()
	if err != nil {
		conn.Close()
		return nil, err
	}
	cipher, err := newAES(key)
	if err != nil {
		conn.Close()
		return nil, err
	}

	c := &Client{
		conn:     conn,
		cipher:   cipher,
		gate:     make(chan struct{}, 1),
		done:     make(chan struct{}),
		host:     hostOf(url),
		clientID: append([]byte(nil), clientID...),
		token:    append([]byte(nil), opts.Token...),
		timeout:  timeout,
	}
	go c.readLoop()

	if err := c.bootstrap(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// bootstrap performs the initial handshake: a zero token encrypted with the
// built-in key, answered with the server build and a session key.
func (c *Client) bootstrap(ctx context.Context) error {
	token := c.token
	if len(token) != 64 {
		token = make([]byte, 64)
	}
	resp, err := c.call(ctx, cmdBootstrap, token)
	if err != nil {
		return err
	}
	if resp.code != 0 {
		return &StatusError{Command: cmdBootstrap, Code: resp.code}
	}
	body := resp.body
	if len(body) < 66 {
		return fmt.Errorf("webterm: short bootstrap reply: %d bytes", len(body))
	}
	build := binary.LittleEndian.Uint16(body[0:2])
	sessionKey := body[66:]

	cipher, err := newAES(sessionKey)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.build = build
	c.cipher = cipher
	c.token = append([]byte(nil), body[2:66]...)
	c.mu.Unlock()
	return nil
}

// TerminalBuild is the server build number reported during the handshake.
func (c *Client) TerminalBuild() uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.build
}

// Token is the session token of the current session.
func (c *Client) Token() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.token...)
}

// Session is the numeric session id of the last login.
func (c *Client) Session() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// Ping sends the heartbeat the terminal expects while the session is idle.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.call(ctx, cmdPing, nil)
	if err != nil {
		return err
	}
	if resp.code != 0 {
		return &StatusError{Command: cmdPing, Code: resp.code}
	}
	return nil
}

// Logout ends the session.
func (c *Client) Logout(ctx context.Context) error {
	resp, err := c.call(ctx, cmdLogout, nil)
	if err != nil {
		return err
	}
	if resp.code != 0 {
		return &StatusError{Command: cmdLogout, Code: resp.code}
	}
	c.mu.Lock()
	c.loggedIn = false
	c.mu.Unlock()
	return nil
}

// Close closes the connection and wakes blocked callers. It is idempotent.
func (c *Client) Close() error {
	c.fail(ErrClosed)
	return nil
}

// hostOf returns the host name of a URL, or an empty string when the URL
// cannot be parsed.
func hostOf(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
