// Package events is the domain-event contract and message-bus transport shared
// by every service on the platform.
//
// Services never talk to each other directly for lifecycle notifications; they
// publish domain events to NATS subjects and any number of consumers react
// asynchronously. This is the spine of the platform's event-driven design: the
// User service doesn't know the Notification service exists, it just publishes
// "user.created" and walks away. Adding a new consumer (analytics, audit,
// indexer) is a deployment, not a code change to producers.
//
// NATS is the transport here; the Bus interface keeps that swappable for Kafka
// or NATS JetStream (durable streams) without touching producer/consumer code.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Subject names. We use a dotted hierarchy so consumers can wildcard-subscribe
// (e.g. "wallet.>" for every wallet event) the way NATS subjects are designed.
const (
	SubjectUserCreated   = "user.created"
	SubjectWalletCreated = "wallet.created"
	SubjectTxSubmitted   = "transaction.submitted"
	SubjectPaymentMade   = "payment.made"
)

// Event is the envelope every message on the bus shares.
type Event struct {
	Subject string          `json:"subject"`
	Time    time.Time       `json:"time"`
	Data    json.RawMessage `json:"data"`
}

// Bus is the minimal publish/subscribe surface the services depend on.
// Implemented by NATSBus; mockable in tests.
type Bus interface {
	Publish(ctx context.Context, subject string, data any) error
	Subscribe(subject string, handler func(Event)) (func() error, error)
	Close()
}

// NATSBus is a NATS-backed Bus.
type NATSBus struct {
	nc *nats.Conn
}

// Connect dials NATS with sane reconnect behaviour so a broker restart doesn't
// take the services down with it.
func Connect(url string) (*NATSBus, error) {
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),            // reconnect forever
		nats.ReconnectWait(2*time.Second), // back off between attempts
	)
	if err != nil {
		return nil, fmt.Errorf("connect nats %q: %w", url, err)
	}
	return &NATSBus{nc: nc}, nil
}

// Publish marshals data into an Event envelope and publishes it to subject.
func (b *NATSBus) Publish(_ context.Context, subject string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}
	evt := Event{Subject: subject, Time: time.Now().UTC(), Data: payload}
	raw, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if err := b.nc.Publish(subject, raw); err != nil {
		return fmt.Errorf("publish %q: %w", subject, err)
	}
	return nil
}

// Subscribe registers handler for every message on subject. The returned
// function unsubscribes. Malformed messages are skipped rather than crashing
// the consumer.
func (b *NATSBus) Subscribe(subject string, handler func(Event)) (func() error, error) {
	sub, err := b.nc.Subscribe(subject, func(msg *nats.Msg) {
		var evt Event
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			return // drop poison message; a real system would dead-letter it
		}
		handler(evt)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe %q: %w", subject, err)
	}
	return sub.Unsubscribe, nil
}

// Close drains and closes the NATS connection.
func (b *NATSBus) Close() {
	if b.nc != nil {
		_ = b.nc.Drain()
	}
}
