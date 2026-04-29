package rabbitmq

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Client is a thin wrapper around an AMQP connection + channel.
type Client struct {
	Conn *amqp.Connection
	Ch   *amqp.Channel
}

// Connect dials the broker, retrying for up to 30s. RabbitMQ takes a
// while to come up in compose, so we don't fail immediately.
func Connect(url string) (*Client, error) {
	var (
		conn *amqp.Connection
		err  error
	)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = amqp.Dial(url)
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	return &Client{Conn: conn, Ch: ch}, nil
}

// Close releases the channel and connection.
func (c *Client) Close() {
	if c.Ch != nil {
		_ = c.Ch.Close()
	}
	if c.Conn != nil {
		_ = c.Conn.Close()
	}
}

// DeclareQueue ensures a durable queue exists.
func (c *Client) DeclareQueue(name string) error {
	_, err := c.Ch.QueueDeclare(name, true, false, false, false, nil)
	return err
}

// Publish sends a JSON message to the named queue.
func (c *Client) Publish(ctx context.Context, queue string, body []byte) error {
	return c.Ch.PublishWithContext(ctx,
		"",    // default exchange
		queue, // routing key = queue name
		false, false,
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
		},
	)
}

// Consume returns a channel of deliveries for the named queue.
func (c *Client) Consume(queue, consumer string) (<-chan amqp.Delivery, error) {
	return c.Ch.Consume(queue, consumer, true, false, false, false, nil)
}
