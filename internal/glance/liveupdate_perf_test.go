package glance

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Timing constants chosen so the full file runs in well under 15 seconds.
// Each test uses at most a few hundred milliseconds.
const (
	// perfTick is the ticker interval used in perf tests.
	perfTick = 50 * time.Millisecond

	// perfCache is the widget cache duration — shorter than perfTick so
	// the widget is always stale when the ticker fires.
	perfCache = 10 * time.Millisecond

	// perfIdle is how long we wait when asserting no work happens.
	// 20 ticks at perfTick each.
	perfIdle = 20 * perfTick

	// perfScaleRatio is the maximum allowed ratio of upstream hits
	// with N clients vs 1 client (tolerance for tick boundary effects).
	// With a server-side ticker, hits should be identical regardless of
	// client count. 1.5× gives generous room for scheduling jitter.
	perfScaleRatio = 1.5
)

// newPerfApp returns an application wired with a fakeWidget whose update
// increments hits. The returned cancel func stops the ticker.
func newPerfApp(t *testing.T, hits *atomic.Int64, pauseWhenIdle bool) (*application, context.CancelFunc) {
	t.Helper()

	app := newTestApp()
	app.Config.Server.LiveUpdates.TickInterval = durationField(perfTick)

	pw := pauseWhenIdle
	app.Config.Server.LiveUpdates.PauseWhenIdle = &pw

	p := &page{}

	fw := newFakeWidget(time.Now().Add(-time.Hour), "<div>content</div>")
	fw.onUpdate = func() {
		hits.Add(1)
		// reset nextUpdate so the widget stays stale for repeated ticks
		fw.nextUpdate = time.Now().Add(-time.Millisecond)
	}
	fw.cacheType = cacheTypeDuration
	fw.nextUpdate = time.Now().Add(-time.Hour)

	p.HeadWidgets = widgets{fw}
	app.slugToPage["test"] = p

	ctx, cancel := context.WithCancel(context.Background())
	app.startLiveUpdateTicker(ctx)
	return app, cancel
}

// registerNClients registers n fake SSE clients and returns a cleanup func.
func registerNClients(hub *eventHub, n int) func() {
	chs := make([]chan event, n)
	for i := range n {
		chs[i] = hub.register()
	}
	return func() {
		for _, ch := range chs {
			hub.unregister(ch)
		}
	}
}

// waitForClientCount polls until hub has exactly n clients or deadline passes.
func waitForClientCount(hub *eventHub, n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if hub.clientCount() == n {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestLiveUpdatePerf_IdleMakesNoUpstreamRequests verifies that with no SSE
// clients connected, the ticker does zero widget fetches.
func TestLiveUpdatePerf_IdleMakesNoUpstreamRequests(t *testing.T) {
	var hits atomic.Int64
	_, cancel := newPerfApp(t, &hits, true)
	defer cancel()

	// Wait for ticker to run multiple times with no clients.
	time.Sleep(perfIdle)

	got := hits.Load()
	if got != 0 {
		t.Errorf("expected 0 upstream requests with no clients (idle gate), got %d", got)
	}
}

// TestLiveUpdatePerf_PauseDisabledStillTicks verifies that pause-when-idle:false
// causes the ticker to run even without clients. This proves test 1 is
// actually measuring the idle gate rather than the ticker being broken.
func TestLiveUpdatePerf_PauseDisabledStillTicks(t *testing.T) {
	var hits atomic.Int64
	_, cancel := newPerfApp(t, &hits, false)
	defer cancel()

	time.Sleep(perfIdle)

	got := hits.Load()
	if got == 0 {
		t.Errorf("expected >0 upstream requests with pause-when-idle=false, got %d — ticker may be broken", got)
	}
}

// TestLiveUpdatePerf_LoadIndependentOfClientCount verifies that N clients
// connected does not multiply upstream requests — the server-side ticker fires
// once per interval regardless of how many browsers are watching.
func TestLiveUpdatePerf_LoadIndependentOfClientCount(t *testing.T) {
	const measureDuration = 500 * time.Millisecond

	var hits1 atomic.Int64
	app1, cancel1 := newPerfApp(t, &hits1, true)
	defer cancel1()

	disconnect1 := registerNClients(app1.hub, 1)
	if !waitForClientCount(app1.hub, 1, 2*time.Second) {
		t.Fatal("timed out waiting for 1 SSE client")
	}
	time.Sleep(measureDuration)
	h1 := hits1.Load()
	disconnect1()

	var hits5 atomic.Int64
	app5, cancel5 := newPerfApp(t, &hits5, true)
	defer cancel5()

	disconnect5 := registerNClients(app5.hub, 5)
	if !waitForClientCount(app5.hub, 5, 2*time.Second) {
		t.Fatal("timed out waiting for 5 SSE clients")
	}
	time.Sleep(measureDuration)
	h5 := hits5.Load()
	disconnect5()

	if h1 == 0 {
		t.Fatal("1-client test produced 0 hits — ticker is not running")
	}

	ratio := float64(h5) / float64(h1)
	if ratio > perfScaleRatio {
		t.Errorf("5 clients produced %d upstream requests vs 1 client's %d (ratio %.2f > %.2f) — load scales with client count",
			h5, h1, ratio, perfScaleRatio)
	}
}

// TestLiveUpdatePerf_RespectsWidgetCache verifies that a widget with a long
// cache is not fetched on every tick.
func TestLiveUpdatePerf_RespectsWidgetCache(t *testing.T) {
	app := newTestApp()
	pw := true
	app.Config.Server.LiveUpdates.PauseWhenIdle = &pw
	app.Config.Server.LiveUpdates.TickInterval = durationField(perfTick)

	var hits atomic.Int64
	p := &page{}

	// Widget with 1s cache — should update at most twice in 1.5s.
	fw := newFakeWidget(time.Now().Add(-time.Hour), "<div>initial</div>")
	fw.cacheType = cacheTypeDuration
	fw.nextUpdate = time.Now().Add(-time.Hour)
	fw.onUpdate = func() {
		hits.Add(1)
		fw.nextUpdate = time.Now().Add(time.Second) // 1s cache
	}

	p.HeadWidgets = widgets{fw}
	app.slugToPage["test"] = p

	ch := app.hub.register()
	defer app.hub.unregister(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.startLiveUpdateTicker(ctx)

	time.Sleep(2 * time.Second) // 40 ticks at 50ms
	got := hits.Load()

	// With 1s cache and 2s window: expect 2-3 updates, not ~40.
	if got > 4 {
		t.Errorf("widget with 1s cache got %d updates in 2s (expected ≤4) — cache not respected", got)
	}
	if got == 0 {
		t.Errorf("widget with 1s cache got 0 updates in 2s — ticker may not be running")
	}
}

// TestLiveUpdatePerf_DisconnectStopsWork verifies that after all SSE clients
// disconnect, the ticker stops making upstream requests.
func TestLiveUpdatePerf_DisconnectStopsWork(t *testing.T) {
	var hits atomic.Int64
	app, cancel := newPerfApp(t, &hits, true)
	defer cancel()

	disconnect := registerNClients(app.hub, 1)
	if !waitForClientCount(app.hub, 1, 2*time.Second) {
		t.Fatal("timed out waiting for client")
	}

	time.Sleep(perfIdle)
	before := hits.Load()
	if before == 0 {
		t.Fatal("expected >0 hits before disconnect — ticker may not be running")
	}

	disconnect()
	if !waitForClientCount(app.hub, 0, 2*time.Second) {
		t.Fatal("timed out waiting for client to disconnect")
	}

	hits.Store(0)
	time.Sleep(perfIdle)
	after := hits.Load()

	if after != 0 {
		t.Errorf("expected 0 hits after all clients disconnected, got %d", after)
	}
}
