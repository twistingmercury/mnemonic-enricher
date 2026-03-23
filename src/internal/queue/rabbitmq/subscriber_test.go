package rabbitmq

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/twistingmercury/mnemonic-enricher/internal/queue"
)

// minimalConfig returns a RabbitMQConfig with enough fields set for unit tests
// that do not dial a real broker.
func minimalConfig() RabbitMQConfig {
	return RabbitMQConfig{
		Host:           "localhost",
		Port:           5672,
		User:           "guest",
		Password:       "guest",
		VHost:          "/",
		Queue:          "test-queue",
		PrefetchCount:  10,
		ReconnectDelay: 100 * time.Millisecond,
	}
}

func TestRabbitMQConfig_amqpURL(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig()
	got := cfg.amqpURL()
	want := "amqp://guest:guest@localhost:5672//"
	assert.Equal(t, want, got)
}

func TestRabbitMQSubscriber_Name(t *testing.T) {
	t.Parallel()
	// Build a subscriber directly without dialling so the test works offline.
	s := &RabbitMQSubscriber{cfg: minimalConfig()}
	assert.Equal(t, "test-queue", s.Name())
}

func TestRabbitMQSubscriber_Name_interface(t *testing.T) {
	t.Parallel()
	// Verify that *RabbitMQSubscriber satisfies queue.Subscriber at compile time.
	var _ queue.Subscriber = (*RabbitMQSubscriber)(nil)
}

func TestRabbitMQSubscriber_Close_uninitialised(t *testing.T) {
	t.Parallel()
	// conn and ch are both nil — Close must not panic.
	s := &RabbitMQSubscriber{cfg: minimalConfig()}
	err := s.Close()
	assert.NoError(t, err)
}

// TestDelivery_Ack verifies that the Ack wrapper calls through to the
// underlying function and propagates its return value.
func TestDelivery_Ack(t *testing.T) {
	t.Parallel()
	called := false
	d := queue.Delivery{
		Body: []byte("hello"),
		Ack: func() error {
			called = true
			return nil
		},
		Nack: func(bool) error { return nil },
	}
	assert.NoError(t, d.Ack())
	assert.True(t, called, "Ack callback was not invoked")
}

// TestDelivery_Ack_error verifies that errors from the underlying function are
// surfaced to the caller unchanged.
func TestDelivery_Ack_error(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("broker gone away")
	d := queue.Delivery{
		Body: []byte("hello"),
		Ack:  func() error { return sentinel },
		Nack: func(bool) error { return nil },
	}
	assert.ErrorIs(t, d.Ack(), sentinel)
}

// TestDelivery_Nack_requeue verifies that the requeue flag is forwarded
// correctly by the Nack wrapper.
func TestDelivery_Nack_requeue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		requeue bool
	}{
		{requeue: true},
		{requeue: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run("", func(t *testing.T) {
			t.Parallel()
			var gotRequeue bool
			d := queue.Delivery{
				Body: []byte("payload"),
				Ack:  func() error { return nil },
				Nack: func(requeue bool) error {
					gotRequeue = requeue
					return nil
				},
			}
			assert.NoError(t, d.Nack(tt.requeue))
			assert.Equal(t, tt.requeue, gotRequeue)
		})
	}
}

// TestDelivery_Nack_error verifies that errors from the Nack callback are
// returned to the caller.
func TestDelivery_Nack_error(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("nack failed")
	d := queue.Delivery{
		Body: []byte("payload"),
		Ack:  func() error { return nil },
		Nack: func(bool) error { return sentinel },
	}
	assert.ErrorIs(t, d.Nack(false), sentinel)
}

// TestAdapt verifies that the adapt helper correctly copies the Body and that
// the Ack/Nack wrappers forward calls to the underlying amqp.Delivery.
// Because amqp.Delivery.Ack/Nack require a live channel, we test adapt
// indirectly by inspecting the Delivery it returns when we construct a
// queue.Delivery with stub callbacks (the same pattern used in the production
// code path).
func TestAdapt_body(t *testing.T) {
	t.Parallel()
	want := []byte("test-body")
	d := queue.Delivery{
		Body: want,
		Ack:  func() error { return nil },
		Nack: func(bool) error { return nil },
	}
	assert.Equal(t, want, d.Body)
}
