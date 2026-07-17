package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/wuxi/sub2proxy/internal/model"
)

// SubscriptionInput is the mutable part of a subscription from the API.
type SubscriptionInput struct {
	Name            string `json:"name"`
	URL             string `json:"url"`
	UserAgent       string `json:"user_agent"`
	RefreshInterval string `json:"refresh_interval"`
}

// Subscriptions returns a copy of all subscriptions with runtime status. The
// result is always non-nil so it serializes to a JSON array, never null.
func (a *App) Subscriptions() []model.Subscription {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]model.Subscription, 0, len(a.cfg.Subscriptions))
	return append(out, a.cfg.Subscriptions...)
}

// AddSubscription validates and stores a new subscription, then synchronously
// fetches it once, returning the deduped node count.
func (a *App) AddSubscription(in SubscriptionInput) (model.Subscription, int, error) {
	if err := validateSubInput(in); err != nil {
		return model.Subscription{}, 0, err
	}
	a.mu.Lock()
	for _, s := range a.cfg.Subscriptions {
		if s.URL == in.URL {
			a.mu.Unlock()
			return model.Subscription{}, 0, conflict(fmt.Sprintf("subscription %q already uses this URL", s.Name))
		}
	}
	sub := model.Subscription{
		ID: genID(), Name: in.Name, URL: in.URL,
		UserAgent: in.UserAgent, RefreshInterval: in.RefreshInterval,
	}
	a.cfg.Subscriptions = append(a.cfg.Subscriptions, sub)
	a.mu.Unlock()

	a.store.Schedule()
	a.startWorker(sub.ID)

	res := a.refreshOne(context.Background(), sub.ID)
	return a.subByID(sub.ID), res.nodeCount, res.err
}

// UpdateSubscription applies mutable fields to an existing subscription.
func (a *App) UpdateSubscription(id string, in SubscriptionInput) (model.Subscription, error) {
	if err := validateSubInput(in); err != nil {
		return model.Subscription{}, err
	}
	a.mu.Lock()
	idx := a.subIndex(id)
	if idx < 0 {
		a.mu.Unlock()
		return model.Subscription{}, notFound("subscription not found")
	}
	for i, s := range a.cfg.Subscriptions {
		if i != idx && s.URL == in.URL {
			a.mu.Unlock()
			return model.Subscription{}, conflict(fmt.Sprintf("subscription %q already uses this URL", s.Name))
		}
	}
	s := &a.cfg.Subscriptions[idx]
	s.Name, s.URL, s.UserAgent, s.RefreshInterval = in.Name, in.URL, in.UserAgent, in.RefreshInterval
	a.mu.Unlock()

	a.store.Schedule()
	a.resetWorker(id)
	return a.subByID(id), nil
}

// DeleteSubscription removes a subscription and its exclusive nodes, then reloads.
func (a *App) DeleteSubscription(id string) error {
	a.mu.Lock()
	idx := a.subIndex(id)
	if idx < 0 {
		a.mu.Unlock()
		return notFound("subscription not found")
	}
	a.cfg.Subscriptions = append(a.cfg.Subscriptions[:idx], a.cfg.Subscriptions[idx+1:]...)
	a.mu.Unlock()

	a.stopWorker(id)
	a.pool.RemoveSubscription(id)
	a.saveCache()
	a.persistAndReload()
	return nil
}

// RefreshSubscription forces a synchronous refresh and resets the worker timer.
func (a *App) RefreshSubscription(id string) (int, error) {
	a.mu.RLock()
	exists := a.subIndex(id) >= 0
	a.mu.RUnlock()
	if !exists {
		return 0, notFound("subscription not found")
	}
	res := a.refreshOne(context.Background(), id)
	a.resetWorker(id)
	return res.nodeCount, res.err
}

type refreshResult struct {
	nodeCount int
	err       error
}

// refreshOne fetches a subscription and updates the pool and runtime status. On
// failure it preserves the previous nodes and records the error.
func (a *App) refreshOne(ctx context.Context, id string) refreshResult {
	a.mu.RLock()
	idx := a.subIndex(id)
	if idx < 0 {
		a.mu.RUnlock()
		return refreshResult{err: notFound("subscription not found")}
	}
	sub := a.cfg.Subscriptions[idx]
	a.mu.RUnlock()

	result, err := a.fetcher.Fetch(ctx, sub)
	now := nowFunc()

	a.mu.Lock()
	idx = a.subIndex(id) // re-find; may have changed
	if idx < 0 {
		a.mu.Unlock()
		return refreshResult{err: notFound("subscription not found")}
	}
	s := &a.cfg.Subscriptions[idx]
	if err != nil {
		s.LastError = err.Error()
		s.LastRefresh = now
		a.mu.Unlock()
		a.log.Warn("refresh subscription failed", "id", id, "err", err)
		return refreshResult{err: err}
	}
	s.LastError = ""
	s.LastRefresh = now
	s.Quota = result.Quota
	s.NodeCount = len(result.Proxies)
	a.mu.Unlock()

	a.pool.SetSubscriptionNodes(id, result.Proxies, now)
	a.saveCache()
	a.persistAndReload()
	for _, w := range result.Warnings {
		a.log.Warn("subscription parse warning", "id", id, "warn", w)
	}
	return refreshResult{nodeCount: len(result.Proxies)}
}

// --- worker scheduling (2.5) ---

type subWorker struct {
	id    string
	reset chan struct{}
	stop  chan struct{}
}

func (a *App) startWorker(id string) {
	a.wmu.Lock()
	if _, ok := a.workers[id]; ok {
		a.wmu.Unlock()
		return
	}
	w := &subWorker{id: id, reset: make(chan struct{}, 1), stop: make(chan struct{})}
	a.workers[id] = w
	a.wmu.Unlock()
	go a.runWorker(w)
}

func (a *App) stopWorker(id string) {
	a.wmu.Lock()
	if w, ok := a.workers[id]; ok {
		close(w.stop)
		delete(a.workers, id)
	}
	a.wmu.Unlock()
}

func (a *App) resetWorker(id string) {
	a.wmu.Lock()
	w := a.workers[id]
	a.wmu.Unlock()
	if w != nil {
		select {
		case w.reset <- struct{}{}:
		default:
		}
	}
}

// runWorker periodically refreshes one subscription. A reset signal (manual
// refresh) restarts the interval, so the next automatic refresh is a full
// interval away (subscription-management spec).
func (a *App) runWorker(w *subWorker) {
	for {
		timer := time.NewTimer(a.subInterval(w.id))
		select {
		case <-a.ctx.Done():
			timer.Stop()
			return
		case <-w.stop:
			timer.Stop()
			return
		case <-w.reset:
			timer.Stop()
			// The manual refresh already ran; just restart the interval.
		case <-timer.C:
			a.refreshOne(a.ctx, w.id)
		}
	}
}

func (a *App) subInterval(id string) time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	idx := a.subIndex(id)
	if idx < 0 {
		return model.DefaultRefreshInterval
	}
	d, err := a.cfg.Subscriptions[idx].Interval()
	if err != nil || d < model.MinRefreshInterval {
		return model.DefaultRefreshInterval
	}
	return d
}

// --- helpers ---

func (a *App) subIndex(id string) int {
	for i := range a.cfg.Subscriptions {
		if a.cfg.Subscriptions[i].ID == id {
			return i
		}
	}
	return -1
}

func (a *App) subByID(id string) model.Subscription {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if idx := a.subIndex(id); idx >= 0 {
		return a.cfg.Subscriptions[idx]
	}
	return model.Subscription{}
}

func validateSubInput(in SubscriptionInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return badRequest("name is required")
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return badRequest("url must be a valid http(s) URL")
	}
	if err := model.ValidateRefreshInterval(in.RefreshInterval); err != nil {
		return badRequest("refresh_interval: " + err.Error())
	}
	return nil
}

func genID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
