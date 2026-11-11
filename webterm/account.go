package webterm

import "context"

// Account returns the account summary: name, server, balance and the
// permissions of the logged in account.
func (c *Client) Account(ctx context.Context) (*Account, error) {
	resp, err := c.call(ctx, cmdAccount, nil)
	if err != nil {
		return nil, err
	}
	if resp.code != 0 {
		return nil, &StatusError{Command: cmdAccount, Code: resp.code}
	}
	values, err := parse(resp.body, accountSchema, 0)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	login := c.login
	c.mu.Unlock()
	acc := newAccount(login, values)
	return &acc, nil
}
