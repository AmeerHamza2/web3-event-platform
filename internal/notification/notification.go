// Package notification is the Notification service: a pure event consumer. It
// subscribes to platform domain events and reacts (here: structured "would
// send" logs standing in for email/SMS/webhook delivery).
//
// It is the proof that the architecture is event-driven, not request-driven:
// the User and Wallet services never call this service, never know it exists,
// and never block on it. Notifications are decoupled work triggered off the bus.
package notification

import (
	"encoding/json"
	"log/slog"

	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
)

// Consumer wires bus subscriptions to delivery handlers.
type Consumer struct {
	bus  events.Bus
	log  *slog.Logger
	subs []func() error
}

// NewConsumer builds a notification consumer.
func NewConsumer(bus events.Bus, log *slog.Logger) *Consumer {
	return &Consumer{bus: bus, log: log}
}

// Start subscribes to the events this service cares about. The "user.>" and
// "wallet.>" wildcards mean new event types in those domains are picked up
// without code changes here.
func (c *Consumer) Start() error {
	subjects := []string{
		events.SubjectUserCreated,
		events.SubjectWalletCreated,
		events.SubjectTxSubmitted,
		events.SubjectPaymentMade,
	}
	for _, subj := range subjects {
		unsub, err := c.bus.Subscribe(subj, c.handle)
		if err != nil {
			return err
		}
		c.subs = append(c.subs, unsub)
		c.log.Info("subscribed", slog.String("subject", subj))
	}
	return nil
}

// Stop unsubscribes everything.
func (c *Consumer) Stop() {
	for _, unsub := range c.subs {
		_ = unsub()
	}
}

func (c *Consumer) handle(e events.Event) {
	// In production this would format and dispatch an email/SMS/webhook. Here we
	// log the delivery so the event flow is observable end-to-end in `compose`.
	c.log.Info("notification dispatched",
		slog.String("event", e.Subject),
		slog.Time("event_time", e.Time),
		slog.String("payload", string(json.RawMessage(e.Data))),
	)
}
