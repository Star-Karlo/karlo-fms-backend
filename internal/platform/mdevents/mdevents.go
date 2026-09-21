// Package mdevents turns master-data change notices into sync wake-ups.
//
// The projections (vehiclesync, driversync) refresh on a ticker, which put a
// truck renamed in TMS up to five minutes away from the FMS Vehicles page.
// Master data — or TMS on its behalf — publishes a notice on the platform
// Redis after a write, and every subscriber refreshes at once. The channel
// carries no payload contract worth parsing: any message means "something
// changed", and a full pass is a handful of gRPC lists.
//
// Same posture as the revocation feed (authctx.Revocations): the follower
// reconnects with backoff and the ticker remains the safety net, so a Redis
// outage costs freshness, not correctness.
package mdevents

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// Channel is the pub/sub channel master data publishes change notices on.
const Channel = "karlo:masterdata"

// Waker is anything that can be asked to refresh; Wake must not block.
type Waker interface{ Wake() }

// Follow subscribes to Channel on addr and wakes every waker on each message.
// Returns at once; empty addr is a no-op.
func Follow(ctx context.Context, addr string, wakers ...Waker) {
	if addr == "" || len(wakers) == 0 {
		return
	}
	go func() {
		backoff := time.Second
		for ctx.Err() == nil {
			err := run(ctx, addr, wakers)
			if ctx.Err() != nil {
				return
			}
			log.Printf("mdevents: %v; reconnecting in %s", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
	}()
}

func run(ctx context.Context, addr string, wakers []Waker) error {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	sub := rdb.Subscribe(ctx, Channel)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		return err
	}
	log.Printf("mdevents: following %s", Channel)
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-ch:
			if !ok {
				return context.Canceled
			}
			log.Printf("mdevents: change notice %q — refreshing projections", m.Payload)
			for _, w := range wakers {
				w.Wake()
			}
		}
	}
}
