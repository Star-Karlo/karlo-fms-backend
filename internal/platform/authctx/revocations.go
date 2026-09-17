package authctx

// Revocations: the platform's live "this token is dead" list, so a logout,
// a suspension or an entitlement change bites on the NEXT request rather
// than when the access token expires. Contract shared with every TMS
// service (their RequireAuthWithRevocations):
//
//	Redis key   revoked:<kind>:<id>  value {"kind":..,"id":..,"at":RFC3339}
//	            kind = session (id = jti) | user (id = uid) | company (id = cid)
//	            TTL = the refresh token life (720h)
//	Channel     karlo:revocations, same JSON, published on each revocation
//
// Rule: reject when kind=session and id == jti; kind=user and id == uid and
// the token was issued before `at`; kind=company likewise on cid. The list
// is warmed with SCAN revoked:* at start and then followed on the channel,
// so the check is an in-memory map lookup per request. Redis being down is
// not fatal: the service keeps verifying signatures and expiry as before,
// and logs once.

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type revocation struct {
	Kind string    `json:"kind"`
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
}

// Revocations is the in-memory list, safe for concurrent use.
type Revocations struct {
	mu       sync.RWMutex
	sessions map[string]struct{}
	users    map[string]time.Time
	orgs     map[string]time.Time
}

func NewRevocations() *Revocations {
	return &Revocations{sessions: map[string]struct{}{}, users: map[string]time.Time{}, orgs: map[string]time.Time{}}
}

func (r *Revocations) add(rv revocation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch rv.Kind {
	case "session":
		r.sessions[rv.ID] = struct{}{}
	case "user":
		if rv.At.After(r.users[rv.ID]) {
			r.users[rv.ID] = rv.At
		}
	case "company":
		if rv.At.After(r.orgs[rv.ID]) {
			r.orgs[rv.ID] = rv.At
		}
	}
}

// Revoked reports whether a verified token has been revoked.
func (r *Revocations) Revoked(p Principal, issuedAt time.Time) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p.TokenID != "" {
		if _, ok := r.sessions[p.TokenID]; ok {
			return true
		}
	}
	if at, ok := r.users[p.UserID]; ok && issuedAt.Before(at) {
		return true
	}
	if at, ok := r.orgs[p.CompanyID]; ok && issuedAt.Before(at) {
		return true
	}
	return false
}

// Follow warms the list from Redis and keeps it current from the channel
// until ctx ends. Reconnects on failure; never blocks the caller.
func (r *Revocations) Follow(ctx context.Context, addr string) {
	if addr == "" {
		return
	}
	go func() {
		backoff := time.Second
		for ctx.Err() == nil {
			if err := r.run(ctx, addr); err != nil && ctx.Err() == nil {
				log.Printf("revocations: %v (retrying in %s)", err, backoff)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return
				}
				if backoff < time.Minute {
					backoff *= 2
				}
				continue
			}
			backoff = time.Second
		}
	}()
}

func (r *Revocations) run(ctx context.Context, addr string) error {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	// Subscribe FIRST so nothing published during the warm-up scan is missed.
	sub := rdb.Subscribe(ctx, "karlo:revocations")
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		return err
	}
	n := 0
	iter := rdb.Scan(ctx, 0, "revoked:*", 500).Iterator()
	for iter.Next(ctx) {
		val, err := rdb.Get(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		var rv revocation
		if json.Unmarshal([]byte(val), &rv) == nil && rv.Kind != "" {
			r.add(rv)
			n++
		} else if parts := strings.SplitN(strings.TrimPrefix(iter.Val(), "revoked:"), ":", 2); len(parts) == 2 {
			r.add(revocation{Kind: parts[0], ID: parts[1], At: time.Now()})
			n++
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}
	log.Printf("revocations: following karlo:revocations (%d active)", n)
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-ch:
			if !ok {
				return context.Canceled
			}
			var rv revocation
			if json.Unmarshal([]byte(m.Payload), &rv) == nil && rv.Kind != "" {
				r.add(rv)
			}
		}
	}
}
