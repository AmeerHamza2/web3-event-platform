// Package notification consumes platform events off the bus and reacts to them.
// Here the reaction is a structured log standing in for email/SMS/webhook
// delivery.
package notification

import (
	"encoding/json"
	"log/slog"

	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
)

type Consumer struct {
	bus  events.Bus
	log  *slog.Logger
	subs []func() error
}

func NewConsumer(bus events.Bus, log *slog.Logger) *Consumer {
	return &Consumer{bus: bus, log: log}
}

// Start subscribes to the events this service handles.
func (c *Consumer) Start() error {
	subjects := []string{
		events.SubjectUserCreated,
		events.SubjectWalletCreated,
		events.SubjectTxSubmitted,
		events.SubjectPaymentMade,
		events.SubjectChainTransfer,
		events.SubjectChainReorg,
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

func (c *Consumer) Stop() {
	for _, unsub := range c.subs {
		_ = unsub()
	}
}

func (c *Consumer) handle(e events.Event) {
	c.log.Info("notification dispatched",
		slog.String("event", e.Subject),
		slog.Time("event_time", e.Time),
		slog.String("payload", string(json.RawMessage(e.Data))),
	)
}
