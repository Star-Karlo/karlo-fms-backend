// Package mdevents turns master-data change notices into sync wake-ups.
//
// The projections (vehiclesync, driversync) refresh on a ticker, which put a
// truck renamed in TMS up to five minutes away from the FMS Vehicles page.
// Master data — or TMS on its behalf — publishes a notice on the platform
// Redis after a write, and every subscriber refreshes at once. The channel
// carries {"kind":"vehicle"|"driver","company_id":"<uuid>","id":"<objectid>",
// "op":"create"|"update"|"delete"} (masterdata-service 0d5674e), but it is
// not parsed: any message means "something changed", both projections run,
// and a full pass is a handful of gRPC lists. Deliberately both — a driver
// delete also clears that driver from the company's vehicles and announces
// only driver/delete.
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

// CompanyChannel is where the authentication service publishes company
// notices: create, profile/settings update, and the moment a company is
// granted FMS (its tenant id allocated) — the one tenantsync must not wait
// five minutes for.
const CompanyChannel = "karlo:companies"

// Follow subscribes to Channel on addr and wakes every waker on each message.
// Returns at once; empty addr is a no-op.
func Follow(ctx context.Context, addr string, wakers ...Waker) {
	FollowChannel(ctx, addr, Channel, wakers...)
}

// FollowChannel is Follow for any channel.
func FollowChannel(ctx context.Context, addr, channel string, wakers ...Waker) {
	if addr == "" || len(wakers) == 0 {
		return
	}
	go func() {
		backoff := time.Second
		for ctx.Err() == nil {
			err := run(ctx, addr, channel, wakers)
			if ctx.Err() != nil {
				return
			}
			log.Printf("mdevents: %s: %v; reconnecting in %s", channel, err, backoff)
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

func run(ctx context.Context, addr, channel string, wakers []Waker) error {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	sub := rdb.Subscribe(ctx, channel)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		return err
	}
	log.Printf("mdevents: following %s", channel)
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-ch:
			if !ok {
				return context.Canceled
			}
			log.Printf("mdevents: %s notice %q — refreshing projections", channel, m.Payload)
			for _, w := range wakers {
				w.Wake()
			}
		}
	}
}
