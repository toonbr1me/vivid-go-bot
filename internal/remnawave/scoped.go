package remnawave

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ScopedClient keeps development profiles separate even when a panel is shared.
// Panel names and Telegram IDs are translated at this boundary; the application
// and its own database continue to use the real Telegram IDs.
type ScopedClient struct {
	client           *Client
	prefix           string
	offset           int64
	subscriptionBase *url.URL
}

const telegramRange int64 = 10_000_000_000

func NewScoped(client *Client, namespace, subscriptionBase string) (*ScopedClient, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{1,11}$`).MatchString(namespace) {
		return nil, fmt.Errorf("REMNAWAVE_NAMESPACE: use 2–12 lowercase letters, digits or underscores")
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(namespace))
	c := &ScopedClient{client: client, prefix: namespace + "_", offset: (1000 + int64(hash.Sum32()%500000)) * telegramRange}
	if subscriptionBase != "" {
		u, err := url.Parse(subscriptionBase)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("SUBSCRIPTION_BASE_URL must be an HTTPS origin")
		}
		c.subscriptionBase = u
	}
	return c, nil
}

func (c *ScopedClient) telegramID(id *int64) (*int64, error) {
	if id == nil {
		return nil, nil
	}
	if *id <= 0 || *id >= telegramRange {
		return nil, fmt.Errorf("Telegram ID is outside the development namespace range")
	}
	mapped := *id + c.offset
	return &mapped, nil
}

func (c *ScopedClient) user(u User) (User, error) {
	if !strings.HasPrefix(u.Username, c.prefix) {
		return User{}, fmt.Errorf("panel profile is outside REMNAWAVE_NAMESPACE")
	}
	u.Username = strings.TrimPrefix(u.Username, c.prefix)
	if u.TelegramID != nil {
		id := *u.TelegramID - c.offset
		if id <= 0 || id >= telegramRange {
			return User{}, fmt.Errorf("panel Telegram ID is outside REMNAWAVE_NAMESPACE")
		}
		u.TelegramID = &id
	}
	if c.subscriptionBase != nil && u.SubscriptionURL != "" {
		link, err := url.Parse(u.SubscriptionURL)
		if err != nil {
			return User{}, err
		}
		link.Scheme, link.Host = c.subscriptionBase.Scheme, c.subscriptionBase.Host
		u.SubscriptionURL = link.String()
	}
	return u, nil
}

// Check every ID-based operation against the panel, including after a restart.
func (c *ScopedClient) owns(ctx context.Context, id int64) error {
	data, err := c.client.do(ctx, http.MethodGet, fmt.Sprintf("/api/users/%d", id), nil, nil, nil)
	if err != nil {
		return err
	}
	u, err := decodeUser(data["response"])
	if err != nil {
		return err
	}
	if u.ID != id {
		return fmt.Errorf("panel returned another profile")
	}
	_, err = c.user(u)
	return err
}

func (c *ScopedClient) UsersByTelegramID(ctx context.Context, id int64) ([]User, error) {
	mapped, err := c.telegramID(&id)
	if err != nil {
		return nil, err
	}
	users, err := c.client.UsersByTelegramID(ctx, *mapped)
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(users))
	for _, raw := range users {
		u, err := c.user(raw)
		if err == nil && u.TelegramID != nil && *u.TelegramID == id {
			out = append(out, u)
		}
	}
	return out, nil
}

func (c *ScopedClient) UserByUsername(ctx context.Context, name string) (*User, error) {
	u, err := c.client.UserByUsername(ctx, c.prefix+name)
	if err != nil || u == nil {
		return nil, err
	}
	user, err := c.user(*u)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *ScopedClient) CreateUser(ctx context.Context, p CreateUserParams) (User, error) {
	var err error
	p.TelegramID, err = c.telegramID(p.TelegramID)
	if err != nil {
		return User{}, err
	}
	p.Username = c.prefix + p.Username
	u, err := c.client.CreateUser(ctx, p)
	if err != nil {
		return User{}, err
	}
	return c.user(u)
}

func (c *ScopedClient) UpdateUser(ctx context.Context, p UpdateUserParams) (User, error) {
	if err := c.owns(ctx, p.UserID); err != nil {
		return User{}, err
	}
	var err error
	p.TelegramID, err = c.telegramID(p.TelegramID)
	if err != nil {
		return User{}, err
	}
	u, err := c.client.UpdateUser(ctx, p)
	if err != nil {
		return User{}, err
	}
	return c.user(u)
}

func (c *ScopedClient) UpdateExpireAt(ctx context.Context, id int64, expiry time.Time, squads []string) (User, error) {
	if err := c.owns(ctx, id); err != nil {
		return User{}, err
	}
	u, err := c.client.UpdateExpireAt(ctx, id, expiry, squads)
	if err != nil {
		return User{}, err
	}
	return c.user(u)
}

func (c *ScopedClient) HWIDDevices(ctx context.Context, id int64) ([]HWIDDevice, error) {
	if err := c.owns(ctx, id); err != nil {
		return nil, err
	}
	return c.client.HWIDDevices(ctx, id)
}

func (c *ScopedClient) DeleteHWIDDevice(ctx context.Context, id int64, hwid string) (bool, error) {
	if err := c.owns(ctx, id); err != nil {
		return false, err
	}
	return c.client.DeleteHWIDDevice(ctx, id, hwid)
}

func (c *ScopedClient) ResetTraffic(ctx context.Context, id int64) error {
	if err := c.owns(ctx, id); err != nil {
		return err
	}
	return c.client.ResetTraffic(ctx, id)
}
