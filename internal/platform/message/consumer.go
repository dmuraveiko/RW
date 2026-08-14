package message

import (
	"context"
	"fmt"
	"sync"

	"github.com/dmuraveiko/RW/internal/platform/config"
	"github.com/nats-io/nats.go"
)

type Handler func(context.Context, string, []byte) error

type Consumer struct {
	subscription *nats.Subscription
	messages     chan *nats.Msg
	workers      sync.WaitGroup
	cancel       context.CancelFunc
}

func StartConsumer(ctx context.Context, connection *nats.Conn, subject, queue string, cfg config.NATS, concurrency int, handler Handler) (*Consumer, error) {
	messages := make(chan *nats.Msg, cfg.PendingMessages)
	workerCtx, cancel := context.WithCancel(ctx)
	subscription, err := connection.QueueSubscribe(subject, queue, func(item *nats.Msg) {
		select {
		case messages <- item:
		case <-workerCtx.Done():
		}
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("subscribe to %s: %w", subject, err)
	}
	if err = subscription.SetPendingLimits(cfg.PendingMessages, cfg.PendingBytes); err != nil {
		cancel()
		_ = subscription.Unsubscribe()
		return nil, fmt.Errorf("set subscription limits: %w", err)
	}
	if err = connection.Flush(); err != nil {
		cancel()
		_ = subscription.Unsubscribe()
		return nil, fmt.Errorf("flush subscription: %w", err)
	}
	consumer := &Consumer{subscription: subscription, messages: messages, cancel: cancel}
	for range concurrency {
		consumer.workers.Add(1)
		go func() {
			defer consumer.workers.Done()
			for {
				select {
				case <-workerCtx.Done():
					return
				case item, ok := <-messages:
					if !ok {
						return
					}
					_ = handler(workerCtx, item.Subject, item.Data)
				}
			}
		}()
	}
	return consumer, nil
}

func (c *Consumer) Stop() {
	c.cancel()
	_ = c.subscription.Unsubscribe()
	c.workers.Wait()
}
