package glance

import (
	"context"
	"sync"
	"time"
)

func (a *application) startLiveUpdateTicker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Duration(a.Config.Server.LiveUpdates.TickInterval))
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.tickAllPages(ctx)
			}
		}
	}()
}

func (a *application) pauseWhenIdle() bool {
	return a.Config.Server.LiveUpdates.PauseWhenIdle != nil && *a.Config.Server.LiveUpdates.PauseWhenIdle
}

func (a *application) tickAllPages(ctx context.Context) {
	if a.pauseWhenIdle() && a.hub.clientCount() == 0 {
		return
	}

	seen := make(map[*page]struct{})
	for _, p := range a.slugToPage {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		a.tickPage(ctx, p)
	}
}

func (a *application) tickPage(ctx context.Context, p *page) {
	now := time.Now()

	// Brief scan: collect stale widget candidates without holding the lock
	// during the actual update calls, so the page handler can acquire p.mu
	// between individual widget updates.
	p.mu.Lock()
	var candidates []widget
	for _, w := range p.HeadWidgets {
		if w.requiresUpdate(&now) {
			candidates = append(candidates, w)
		}
	}
	for c := range p.Columns {
		for _, w := range p.Columns[c].Widgets {
			if w.requiresUpdate(&now) {
				candidates = append(candidates, w)
			}
		}
	}
	p.mu.Unlock()

	if len(candidates) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, w := range candidates {
		wg.Add(1)
		go func(w widget) {
			defer wg.Done()

			// Each goroutine acquires p.mu independently. The page handler
			// can therefore acquire p.mu between individual widget updates
			// rather than waiting for all updates to complete.
			p.mu.Lock()
			// Double-check: the page handler may have already updated this
			// widget between the scan above and now.
			if !w.requiresUpdate(&now) {
				p.mu.Unlock()
				return
			}
			prevHTML := w.Render()
			w.update(ctx)
			newHTML := w.Render()
			p.mu.Unlock()

			if newHTML != prevHTML {
				a.hub.publish(event{
					Type:     "widget-updated",
					WidgetID: w.GetID(),
					Time:     now,
				})
			}
		}(w)
	}
	wg.Wait()
}
