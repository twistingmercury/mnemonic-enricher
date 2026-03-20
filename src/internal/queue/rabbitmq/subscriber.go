// Package rabbitmq provides a RabbitMQ implementation of the queue.Subscriber
// interface backed by github.com/rabbitmq/amqp091-go.
package rabbitmq

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"

	"github.com/twistingmercury/mnemonic-enricher/internal/queue"
)

// RabbitMQConfig holds the connection and consumer settings for a RabbitMQ
// subscriber. It is defined here so that Cycle 3 is self-contained; Cycle 4
// will migrate it into the central config package.
type RabbitMQConfig struct {
	Host           string
	Port           int
	User           string
	Password       string // #nosec G117 -- field holds a credential by design; value comes from env/secret manager, never hardcoded
	VHost          string
	Queue          string
	PrefetchCount  int
	ReconnectDelay time.Duration
}

// amqpURL builds an AMQP connection URL from the configuration fields.
func (c RabbitMQConfig) amqpURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%d/%s", c.User, c.Password, c.Host, c.Port, c.VHost)
}

// RabbitMQSubscriber implements queue.Subscriber for RabbitMQ.
type RabbitMQSubscriber struct {
	cfg  RabbitMQConfig
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewSubscriber dials RabbitMQ, sets up the QoS prefetch, and returns a
// ready-to-use queue.Subscriber. The caller is responsible for calling Close
// when the subscriber is no longer needed.
func NewSubscriber(cfg RabbitMQConfig) (queue.Subscriber, error) {
	conn, ch, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	return &RabbitMQSubscriber{cfg: cfg, conn: conn, ch: ch}, nil
}

// dial opens a new connection and channel, and configures QoS.
func dial(cfg RabbitMQConfig) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(cfg.amqpURL())
	if err != nil {
		return nil, nil, fmt.Errorf("rabbitmq: dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq: open channel: %w", err)
	}

	if cfg.PrefetchCount > 0 {
		if err = ch.Qos(cfg.PrefetchCount, 0, false); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return nil, nil, fmt.Errorf("rabbitmq: set qos: %w", err)
		}
	}

	return conn, ch, nil
}

// Name returns the queue name from the configuration.
func (s *RabbitMQSubscriber) Name() string {
	return s.cfg.Queue
}

// Close releases the channel and connection in order. Errors from both
// operations are joined; the function always attempts both closes.
func (s *RabbitMQSubscriber) Close() error {
	var chErr, connErr error
	if s.ch != nil {
		chErr = s.ch.Close()
	}
	if s.conn != nil {
		connErr = s.conn.Close()
	}
	if chErr != nil && connErr != nil {
		return fmt.Errorf("rabbitmq: close channel: %w; close connection: %v", chErr, connErr)
	}
	if chErr != nil {
		return fmt.Errorf("rabbitmq: close channel: %w", chErr)
	}
	if connErr != nil {
		return fmt.Errorf("rabbitmq: close connection: %w", connErr)
	}
	return nil
}

// Subscribe starts consuming from the configured queue and returns a channel
// of provider-agnostic queue.Delivery values. The goroutine runs until ctx is
// cancelled. If the broker connection is lost the subscriber reconnects after
// cfg.ReconnectDelay, honouring ctx cancellation during the wait.
func (s *RabbitMQSubscriber) Subscribe(ctx context.Context) (<-chan queue.Delivery, error) {
	out := make(chan queue.Delivery)
	go s.consumeLoop(ctx, out)
	return out, nil
}

// consumeLoop is the long-running goroutine that reads from the broker and
// adapts amqp091.Delivery values to queue.Delivery. On error it reconnects
// with backoff.
func (s *RabbitMQSubscriber) consumeLoop(ctx context.Context, out chan<- queue.Delivery) {
	defer close(out)

	for {
		deliveries, err := s.ch.Consume(
			s.cfg.Queue,
			"",    // consumer tag — let the broker assign one
			false, // auto-ack: false — callers must call Ack/Nack
			false, // exclusive
			false, // no-local
			false, // no-wait
			nil,   // args
		)
		if err != nil {
			log.Error().Err(err).Str("queue", s.cfg.Queue).Msg("rabbitmq: consume failed, will reconnect")
			if !s.waitAndReconnect(ctx) {
				return
			}
			continue
		}

		log.Info().Str("queue", s.cfg.Queue).Msg("rabbitmq: consuming")

		if done := s.drain(ctx, deliveries, out); done {
			return
		}

		// Channel was closed by the broker; attempt reconnect.
		log.Warn().Str("queue", s.cfg.Queue).Msg("rabbitmq: delivery channel closed, reconnecting")
		if !s.waitAndReconnect(ctx) {
			return
		}
	}
}

// drain reads from the amqp delivery channel and forwards adapted messages to
// out. It returns true when ctx is done (caller should exit) and false when
// the amqp channel closed (caller should reconnect).
func (s *RabbitMQSubscriber) drain(ctx context.Context, deliveries <-chan amqp.Delivery, out chan<- queue.Delivery) bool {
	for {
		select {
		case <-ctx.Done():
			return true
		case d, ok := <-deliveries:
			if !ok {
				return false
			}
			adapted := adapt(d)
			select {
			case out <- adapted:
			case <-ctx.Done():
				return true
			}
		}
	}
}

// adapt converts an amqp091.Delivery to a provider-agnostic queue.Delivery.
func adapt(d amqp.Delivery) queue.Delivery {
	return queue.Delivery{
		Body: d.Body,
		Ack: func() error {
			return d.Ack(false)
		},
		Nack: func(requeue bool) error {
			return d.Nack(false, requeue)
		},
	}
}

// waitAndReconnect sleeps for cfg.ReconnectDelay (or until ctx is cancelled)
// then re-dials the broker. It returns false if the caller should stop.
func (s *RabbitMQSubscriber) waitAndReconnect(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(s.cfg.ReconnectDelay):
	}

	conn, ch, err := dial(s.cfg)
	if err != nil {
		log.Error().Err(err).Str("queue", s.cfg.Queue).Msg("rabbitmq: reconnect failed")
		return true // keep the loop alive; next iteration will retry
	}

	// Close stale resources before replacing them.
	if s.ch != nil {
		_ = s.ch.Close()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}

	s.conn = conn
	s.ch = ch
	log.Info().Str("queue", s.cfg.Queue).Msg("rabbitmq: reconnected")
	return true
}
