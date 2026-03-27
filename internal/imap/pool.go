package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/jpennington/llmail/internal/config"
)

const (
	defaultMaxIdle   = 2
	defaultMaxActive = 5
)

type Pool struct {
	cfg *config.Config
	mu  sync.Mutex
	// per-account pools
	idle   map[string][]*imapclient.Client
	active map[string]int
	closed bool

	getPassword func(account string) (string, error)
}

type PoolConfig struct {
	Config      *config.Config
	GetPassword func(account string) (string, error)
}

func NewPool(pc PoolConfig) *Pool {
	return &Pool{
		cfg:         pc.Config,
		idle:        make(map[string][]*imapclient.Client),
		active:      make(map[string]int),
		getPassword: pc.GetPassword,
	}
}

func (p *Pool) Get(ctx context.Context, account string) (*imapclient.Client, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("pool is closed")
	}

	// Try to return an idle connection
	if clients, ok := p.idle[account]; ok && len(clients) > 0 {
		c := clients[len(clients)-1]
		p.idle[account] = clients[:len(clients)-1]
		p.active[account]++
		p.mu.Unlock()

		// Health check with NOOP
		if err := c.Noop().Wait(); err != nil {
			slog.Debug("idle connection failed NOOP, dialing new", "account", account, "err", err)
			_ = c.Close()
			p.mu.Lock()
			p.active[account]--
			p.mu.Unlock()
			return p.dial(ctx, account)
		}
		return c, nil
	}

	if p.active[account] >= defaultMaxActive {
		p.mu.Unlock()
		return nil, fmt.Errorf("max active connections reached for account %q", account)
	}
	p.active[account]++
	p.mu.Unlock()

	c, err := p.dial(ctx, account)
	if err != nil {
		p.mu.Lock()
		p.active[account]--
		p.mu.Unlock()
		return nil, err
	}
	return c, nil
}

func (p *Pool) Put(account string, c *imapclient.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.active[account]--

	if p.closed || len(p.idle[account]) >= defaultMaxIdle {
		_ = c.Close()
		return
	}

	p.idle[account] = append(p.idle[account], c)
}

func (p *Pool) Discard(account string, c *imapclient.Client) {
	_ = c.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active[account]--
}

func (p *Pool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	for account, clients := range p.idle {
		for _, c := range clients {
			_ = c.Close()
		}
		delete(p.idle, account)
	}
}

func (p *Pool) dial(ctx context.Context, account string) (*imapclient.Client, error) {
	acc, ok := p.cfg.GetAccount(account)
	if !ok {
		return nil, fmt.Errorf("unknown account: %s", account)
	}

	password, err := p.getPassword(account)
	if err != nil {
		return nil, fmt.Errorf("getting password for %s: %w", account, err)
	}

	addr := fmt.Sprintf("%s:%d", acc.Host, acc.Port)

	var opts *imapclient.Options
	if acc.TLS {
		opts = &imapclient.Options{
			TLSConfig: &tls.Config{ServerName: acc.Host},
		}
	}

	var c *imapclient.Client
	if acc.TLS {
		c, err = imapclient.DialTLS(addr, opts)
	} else {
		c, err = imapclient.DialInsecure(addr, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}

	if err := c.Login(acc.Username, password).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("authenticating %s: %w", account, err)
	}

	return c, nil
}

// DialAndCheck connects, authenticates, and returns the client + capabilities.
func DialAndCheck(ctx context.Context, host string, port int, useTLS bool, username, password string) (*imapclient.Client, imap.CapSet, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	var opts *imapclient.Options
	if useTLS {
		opts = &imapclient.Options{
			TLSConfig: &tls.Config{ServerName: host},
		}
	}

	var c *imapclient.Client
	var err error
	if useTLS {
		c, err = imapclient.DialTLS(addr, opts)
	} else {
		c, err = imapclient.DialInsecure(addr, opts)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}

	if err := c.Login(username, password).Wait(); err != nil {
		_ = c.Close()
		return nil, nil, fmt.Errorf("authenticating: %w", err)
	}

	return c, c.Caps(), nil
}
