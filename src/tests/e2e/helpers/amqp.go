package helpers

import (
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	defaultRabbitMQURL = "amqp://guest:guest@localhost:5672/"
	enrichmentQueue    = "enrichment-jobs"
)

// PublishJob publishes {"job_id":"<jobID>"} to the "enrichment-jobs" queue.
// Reads RABBITMQ_URL env var (default: "amqp://guest:guest@localhost:5672/").
// Uses a transient connection that closes after publish.
// Declares the queue as durable before publishing.
// Calls t.Fatal on error.
func PublishJob(t *testing.T, jobID uuid.UUID) {
	t.Helper()

	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		url = defaultRabbitMQURL
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("failed to connect to RabbitMQ at %s: %v", url, err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("failed to open RabbitMQ channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		enrichmentQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		t.Fatalf("failed to declare queue %q: %v", enrichmentQueue, err)
	}

	body := fmt.Sprintf(`{"job_id":"%s"}`, jobID.String())

	err = ch.Publish(
		"",              // exchange (default)
		enrichmentQueue, // routing key
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         []byte(body),
			DeliveryMode: amqp.Persistent,
		},
	)
	if err != nil {
		t.Fatalf("failed to publish job %s to queue %q: %v", jobID, enrichmentQueue, err)
	}
}

// PublishRaw publishes arbitrary bytes to the "enrichment-jobs" queue.
// Reads RABBITMQ_URL env var (default: "amqp://guest:guest@localhost:5672/").
// Uses a transient connection that closes after publish.
// Declares the queue as durable before publishing.
// Calls t.Fatal on error.
func PublishRaw(t *testing.T, body []byte) {
	t.Helper()

	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		url = defaultRabbitMQURL
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("failed to connect to RabbitMQ at %s: %v", url, err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("failed to open RabbitMQ channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		enrichmentQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		t.Fatalf("failed to declare queue %q: %v", enrichmentQueue, err)
	}

	err = ch.Publish(
		"",              // exchange (default)
		enrichmentQueue, // routing key
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
		},
	)
	if err != nil {
		t.Fatalf("failed to publish raw message to queue %q: %v", enrichmentQueue, err)
	}
}
