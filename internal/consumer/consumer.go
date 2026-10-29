// Package consumer receives platform events from RabbitMQ: enhanced gallery images, subscription
// changes and reviews. Every event is processed once, tracked by its id in processed_events.
package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"

	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/server"
)

// CloudEvent is the envelope QueueModule publishes.
type CloudEvent struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Source   string          `json:"source"`
	TenantID string          `json:"tenantid"`
	Data     json.RawMessage `json:"data"`
}

// Subscription binds one durable queue to a set of event types.
type Subscription struct {
	Queue  string
	Events []string
}

// Consumer consumes the vertical's queues with manual acknowledgement and a dead-letter queue per
// subscription.
type Consumer struct {
	app      *server.App
	url      string
	exchange string
	prefetch int
	vertical string
}

// New builds a consumer for the vertical's queues on the given broker and exchange.
func New(app *server.App, url, exchange string, prefetch int) *Consumer {
	return &Consumer{app: app, url: url, exchange: exchange, prefetch: prefetch, vertical: app.Cfg.Platform.Vertical}
}

// Subscriptions lists the queues of this vertical and the events bound to each.
func (c *Consumer) Subscriptions() []Subscription {
	return []Subscription{
		{Queue: c.vertical + ".gallery", Events: []string{"gallery.image_enhanced", "gallery.published"}},
		{Queue: c.vertical + ".subscription", Events: []string{"subscription.activated", "subscription.renewed", "subscription.expiring", "subscription.expired"}},
		{Queue: c.vertical + ".review", Events: []string{"review.created", "review.approved", "review.rejected", "review.deleted"}},
	}
}

// Start declares the queues through QueueModule and consumes in the background, reconnecting
// with exponential backoff. An empty broker URL disables it.
func (c *Consumer) Start(ctx context.Context) {
	if c.url == "" {
		log.Warn().Msg("rabbitmq url empty; event consumer disabled")
		return
	}
	for _, s := range c.Subscriptions() {
		for _, ev := range s.Events {
			topic := ev[:indexDot(ev)]
			if err := c.app.Clients.Events.EnsureQueue(ctx, s.Queue, topic); err != nil {
				log.Debug().Err(err).Str("queue", s.Queue).Msg("EnsureQueue via QueueModule failed (declaring locally)")
			}
		}
	}
	go c.loop(ctx)
}

func indexDot(s string) int {
	for i, r := range s {
		if r == '.' {
			return i
		}
	}
	return len(s)
}

// loop keeps a consumer session alive, doubling the reconnect delay up to 30 seconds.
func (c *Consumer) loop(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.consume(ctx); err != nil {
			log.Warn().Err(err).Dur("retry_in", backoff).Msg("consumer disconnected")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare(c.exchange, "headers", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	if err := ch.Qos(c.prefetch, 0, false); err != nil {
		return err
	}
	deliveries := make(chan amqp.Delivery, c.prefetch)
	for _, s := range c.Subscriptions() {
		dlx, dlq := s.Queue+".dlx", s.Queue+".dlq"
		if err := ch.ExchangeDeclare(dlx, "fanout", true, false, false, false, nil); err != nil {
			return err
		}
		if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
			return err
		}
		if err := ch.QueueBind(dlq, "", dlx, false, nil); err != nil {
			return err
		}
		if _, err := ch.QueueDeclare(s.Queue, true, false, false, false, amqp.Table{"x-dead-letter-exchange": dlx, "x-dead-letter-routing-key": ""}); err != nil {
			return fmt.Errorf("declare %s: %w", s.Queue, err)
		}
		for _, ev := range s.Events {
			if err := ch.QueueBind(s.Queue, "", c.exchange, false, amqp.Table{"x-match": "all", "event_type": ev}); err != nil {
				return fmt.Errorf("bind %s/%s: %w", s.Queue, ev, err)
			}
		}
		msgs, err := ch.Consume(s.Queue, "", false, false, false, false, nil)
		if err != nil {
			return err
		}
		go func() {
			for m := range msgs {
				deliveries <- m
			}
		}()
	}
	log.Info().Str("exchange", c.exchange).Msg("event consumer started")
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-closed:
			return fmt.Errorf("connection closed: %v", err)
		case d := <-deliveries:
			c.handle(ctx, d)
		}
	}
}

// handle processes one delivery. Malformed events go to the dead-letter queue, duplicates are
// acknowledged without work, and transient storage errors are requeued.
func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) {
	var ev CloudEvent
	if err := json.Unmarshal(d.Body, &ev); err != nil {
		log.Error().Err(err).Msg("bad event envelope; dead-lettering")
		_ = d.Nack(false, false)
		return
	}
	if ev.ID != "" {
		fresh, err := c.app.Repos.Events.MarkProcessed(ctx, ev.ID)
		if err != nil {
			log.Error().Err(err).Msg("idempotency check failed; requeue")
			_ = d.Nack(false, true)
			return
		}
		if !fresh {
			_ = d.Ack(false)
			return
		}
	}
	if err := c.dispatch(ctx, ev); err != nil {
		log.Error().Err(err).Str("type", ev.Type).Msg("event handling failed; dead-lettering")
		_ = d.Nack(false, false)
		return
	}
	_ = d.Ack(false)
}

type galleryPayload struct {
	BusinessID     string `json:"business_id"`
	GalleryItemID  string `json:"gallery_item_id"`
	EnhancedFileID string `json:"enhanced_file_id"`
	Channel        string `json:"channel"`
	Vertical       string `json:"vertical"`
}

type subscriptionPayload struct {
	BusinessID string     `json:"business_id"`
	Plan       string     `json:"plan"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Vertical   string     `json:"vertical"`
}

type reviewPayload struct {
	BusinessID  string  `json:"business_id"`
	Rating      float64 `json:"rating"`
	ReviewCount int32   `json:"review_count"`
}

func (c *Consumer) dispatch(ctx context.Context, ev CloudEvent) error {
	switch ev.Type {
	case "gallery.image_enhanced":
		var p galleryPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		if p.Vertical != "" && p.Vertical != c.vertical || p.GalleryItemID == "" {
			return nil
		}
		return ignoreNotFound(c.app.Core.SetGalleryEnhanced(ctx, p.BusinessID, p.GalleryItemID, p.EnhancedFileID))
	case "gallery.published":
		var p galleryPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		if p.Vertical != "" && p.Vertical != c.vertical || p.GalleryItemID == "" {
			return nil
		}
		return ignoreNotFound(c.app.Core.MarkGalleryPublished(ctx, p.BusinessID, p.GalleryItemID, p.Channel))
	case "subscription.activated", "subscription.renewed", "subscription.expiring", "subscription.expired":
		var p subscriptionPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		if p.Vertical != "" && p.Vertical != c.vertical {
			return nil
		}
		b, err := c.app.Repos.Businesses.Get(ctx, p.BusinessID)
		if err != nil {
			return ignoreNotFound(err)
		}
		if p.Plan != "" {
			b.Plan = p.Plan
		}
		b.PlanExpiresAt = p.ExpiresAt
		if ev.Type == "subscription.expired" {
			b.IsActive = false
		}
		return c.app.Repos.Businesses.Update(ctx, b)
	case "review.created", "review.approved", "review.rejected", "review.deleted":
		var p reviewPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		if p.BusinessID == "" {
			return nil
		}
		if p.ReviewCount == 0 && p.Rating == 0 {

			rating, count := c.app.Clients.Reviews.Rating(ctx, p.BusinessID)
			p.Rating, p.ReviewCount = rating, count
		}
		return ignoreNotFound(c.app.Repos.Businesses.SetRating(ctx, p.BusinessID, p.Rating, p.ReviewCount))
	}
	return nil
}

func ignoreNotFound(err error) error {
	if err == nil {
		return nil
	}
	if err.Error() == "not found" {
		return nil
	}
	return err
}
