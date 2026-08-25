package identityclient

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/room"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 800 * time.Millisecond}}
}
func (c *Client) call(ctx context.Context, method, path string, out any) error {
	r, e := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if e != nil {
		return e
	}
	res, e := c.http.Do(r)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("identity-room status=%d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
func (c *Client) Get(ctx context.Context, id int64) (room.Room, error) {
	var v room.Room
	e := c.call(ctx, "GET", fmt.Sprintf("/internal/v1/rooms/%d", id), &v)
	return v, e
}
func (c *Client) access(ctx context.Context, r, u int64) (struct {
	Banned bool `json:"banned"`
	Muted  bool `json:"muted"`
}, error) {
	var v struct {
		Banned bool `json:"banned"`
		Muted  bool `json:"muted"`
	}
	e := c.call(ctx, "GET", fmt.Sprintf("/internal/v1/rooms/%d/access/%d", r, u), &v)
	return v, e
}
func (c *Client) IsBanned(ctx context.Context, r, u int64) (bool, error) {
	v, e := c.access(ctx, r, u)
	return v.Banned, e
}
func (c *Client) IsMuted(ctx context.Context, r, u int64) (bool, error) {
	v, e := c.access(ctx, r, u)
	return v.Muted, e
}
func (c *Client) Join(ctx context.Context, r, u int64) (room.Room, error) {
	var v room.Room
	e := c.call(ctx, "POST", fmt.Sprintf("/internal/v1/rooms/%d/join/%d", r, u), &v)
	return v, e
}
func (c *Client) User(ctx context.Context, id int64) (auth.User, error) {
	var v auth.User
	e := c.call(ctx, "GET", fmt.Sprintf("/internal/v1/users/%d", id), &v)
	return v, e
}
func (c *Client) Create(context.Context, int64, string) (room.Room, error) {
	return room.Room{}, fmt.Errorf("unsupported")
}
func (c *Client) List(context.Context, room.Status, int) ([]room.Room, error) {
	return nil, fmt.Errorf("unsupported")
}
func (c *Client) Start(context.Context, int64, int64) (room.Room, error) {
	return room.Room{}, fmt.Errorf("unsupported")
}
func (c *Client) Stop(context.Context, int64, int64) (room.Room, error) {
	return room.Room{}, fmt.Errorf("unsupported")
}
func (c *Client) Mute(context.Context, int64, int64, int64, time.Duration, string) error {
	return fmt.Errorf("unsupported")
}
func (c *Client) Unmute(context.Context, int64, int64, int64) error { return fmt.Errorf("unsupported") }
func (c *Client) Ban(context.Context, int64, int64, int64, string) error {
	return fmt.Errorf("unsupported")
}
func (c *Client) Unban(context.Context, int64, int64, int64) error { return fmt.Errorf("unsupported") }
