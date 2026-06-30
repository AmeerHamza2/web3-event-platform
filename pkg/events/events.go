// Package events defines the platform's domain-event contract and a NATS-backed
// message bus. Services publish events and react to them asynchronously rather
// than calling each other directly.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Event subjects. The dotted hierarchy lets consumers wildcard-subscribe
// (e.g. "wallet.>").
const (
	SubjectUserCreated   = "user.created"
	SubjectWalletCreated = "wallet.created"
	SubjectTxSubmitted   = "transaction.submitted"
	SubjectPaymentMade   = "payment.made"
	SubjectChainBlock    = "chain.block"
	SubjectChainTransfer = "chain.transfer"
	SubjectChainReorg    = "chain.reorg"
)

// Event is the envelope for every message on the bus.
type Event struct {
	Subject string          `json:"subject"`
	Time    time.Time       `json:"time"`
	Data    json.RawMessage `json:"data"`
}

// Bus is the publish/subscribe surface services depend on. NATSBus is the
// production implementation; tests provide their own.
type Bus interface {
	Publish(ctx context.Context, subject string, data any) error
	Subscribe(subject string, handler func(Event)) (func() error, error)
	Close()
}

// NATSBus is a NATS-backed Bus.
type NATSBus struct {
	nc *nats.Conn
}

// Connect dials NATS with indefinite reconnect so a broker restart doesn't take
// the services down with it.
func Connect(url string) (*NATSBus, error) {
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("connect nats %q: %w", url, err)
	}
	return &NATSBus{nc: nc}, nil
}

// Publish wraps data in an Event envelope and publishes it to subject.
func (b *NATSBus) Publish(_ context.Context, subject string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}
	raw, err := json.Marshal(Event{Subject: subject, Time: time.Now().UTC(), Data: payload})
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if err := b.nc.Publish(subject, raw); err != nil {
		return fmt.Errorf("publish %q: %w", subject, err)
	}
	return nil
}

// Subscribe registers handler for every message on subject. The returned
// function unsubscribes. Malformed messages are dropped.
func (b *NATSBus) Subscribe(subject string, handler func(Event)) (func() error, error) {
	sub, err := b.nc.Subscribe(subject, func(msg *nats.Msg) {
		var evt Event
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			return
		}
		handler(evt)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe %q: %w", subject, err)
	}
	return sub.Unsubscribe, nil
}

// Close drains and closes the connection.
func (b *NATSBus) Close() {
	if b.nc != nil {
		_ = b.nc.Drain()
	}
}
