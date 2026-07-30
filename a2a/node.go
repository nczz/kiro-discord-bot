package a2a

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const defaultDrainTimeout = 10 * time.Second

type NodeConfig struct {
	Config               Config
	DrainTimeout         time.Duration
	ReconnectWait        time.Duration
	MaxReconnects        int
	RetryOnFailedConnect bool
	Logf                 func(string, ...any)
}

type Node struct {
	mu      sync.RWMutex
	enabled bool
	config  NodeConfig
	nc      *nats.Conn
	js      jetstream.JetStream
}

func Connect(ctx context.Context, cfg NodeConfig) (*Node, error) {
	if err := cfg.Config.ValidateStartup(); err != nil {
		return nil, err
	}
	if !cfg.Config.Enabled() {
		return &Node{enabled: false, config: cfg}, nil
	}

	drainTimeout := cfg.DrainTimeout
	if drainTimeout <= 0 {
		drainTimeout = defaultDrainTimeout
	}
	reconnectWait := cfg.ReconnectWait
	if reconnectWait <= 0 {
		reconnectWait = 2 * time.Second
	}
	maxReconnects := cfg.MaxReconnects
	if maxReconnects == 0 {
		maxReconnects = 60
	}

	clientName := strings.TrimSpace(cfg.Config.AgentName)
	if clientName == "" {
		clientName = "kiro-discord-bot:" + string(cfg.Config.AgentID)
	}
	opts := []nats.Option{
		nats.Name(clientName),
		nats.DrainTimeout(drainTimeout),
		nats.ReconnectWait(reconnectWait),
		nats.MaxReconnects(maxReconnects),
		nats.RetryOnFailedConnect(cfg.RetryOnFailedConnect),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logNATS(cfg, "[a2a] nats disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logNATS(cfg, "[a2a] nats reconnected: %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			logNATS(cfg, "[a2a] nats closed")
		}),
	}
	if creds := strings.TrimSpace(cfg.Config.NATSCredsFile); creds != "" {
		opts = append(opts, nats.UserCredentials(creds))
	}
	if token := strings.TrimSpace(cfg.Config.NATSToken); token != "" {
		opts = append(opts, nats.Token(token))
	}
	if ca := strings.TrimSpace(cfg.Config.NATSTLSCAFile); ca != "" {
		opts = append(opts, nats.RootCAs(ca))
	}

	nc, err := nats.Connect(strings.TrimSpace(cfg.Config.NATSURL), opts...)
	if err != nil {
		return nil, fmt.Errorf("connect A2A NATS: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("create A2A JetStream context: %w", err)
	}
	if _, err := js.AccountInfo(ctx); err != nil {
		nc.Close()
		return nil, fmt.Errorf("verify A2A JetStream account: %w", err)
	}
	return &Node{enabled: true, config: cfg, nc: nc, js: js}, nil
}

func (n *Node) IsEnabled() bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.enabled
}

func (n *Node) NATSConn() *nats.Conn {
	if n == nil {
		return nil
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.nc
}

func (n *Node) JetStream() jetstream.JetStream {
	if n == nil {
		return nil
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.js
}

func (n *Node) AgentID() AgentID {
	if n == nil {
		return ""
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.config.Config.AgentID
}

func (n *Node) Publish(ctx context.Context, subject string, payload []byte, natsMsgID string) (*jetstream.PubAck, error) {
	if n == nil || !n.IsEnabled() {
		return nil, fmt.Errorf("A2A NATS node is disabled")
	}
	if strings.TrimSpace(natsMsgID) == "" {
		return nil, fmt.Errorf("Nats-Msg-Id is required")
	}
	js := n.JetStream()
	if js == nil {
		return nil, fmt.Errorf("A2A JetStream is not initialized")
	}
	return js.Publish(ctx, subject, payload, jetstream.WithMsgID(natsMsgID))
}

func (n *Node) Drain(ctx context.Context) error {
	if n == nil || !n.IsEnabled() {
		return nil
	}
	nc := n.NATSConn()
	if nc == nil || nc.IsClosed() {
		return nil
	}
	if err := nc.Drain(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		for !nc.IsClosed() {
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()
	select {
	case <-ctx.Done():
		nc.Close()
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (n *Node) Close() {
	if n == nil {
		return
	}
	if nc := n.NATSConn(); nc != nil {
		nc.Close()
	}
}

func logNATS(cfg NodeConfig, format string, args ...any) {
	if cfg.Logf != nil {
		cfg.Logf(format, args...)
	}
}
