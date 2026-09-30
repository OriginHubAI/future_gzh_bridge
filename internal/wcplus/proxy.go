package wcplus

import (
	"context"
	"encoding/json"
)

// ProxySet POST /api/settings/proxy/set — same as wcplus UI "Set Proxy".
func (c *Client) ProxySet(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.postJSON(ctx, "/api/settings/proxy/set", map[string]any{}, &out)
	return out, err
}

// ProxyUnset POST /api/settings/proxy/unset — same as "Clear Proxy".
func (c *Client) ProxyUnset(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.postJSON(ctx, "/api/settings/proxy/unset", map[string]any{}, &out)
	return out, err
}

// LicenseSettings GET /api/settings/license — Max / 微信自动化等授权信息。
func (c *Client) LicenseSettings(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.getJSON(ctx, "/api/settings/license", nil, &out)
	return out, err
}
