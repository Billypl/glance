package glance

import (
	"context"
	"fmt"
	"html/template"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeWidget struct {
	widgetBase
	updateCount  atomic.Int32
	renderedHTML template.HTML
	onUpdate     func()
}

func newFakeWidget(nextUpdate time.Time, html template.HTML) *fakeWidget {
	w := &fakeWidget{renderedHTML: html}
	w.setID(widgetIDCounter.Add(1))
	w.cacheType = cacheTypeDuration
	w.nextUpdate = nextUpdate
	return w
}

func (w *fakeWidget) initialize() error { return nil }

func (w *fakeWidget) Render() template.HTML {
	return template.HTML(fmt.Sprintf(
		`<div class="widget" data-widget-id="%d" data-next-update="%d">%s</div>`,
		w.GetID(), w.NextUpdateUnixMilli(), w.renderedHTML,
	))
}

func (w *fakeWidget) update(ctx context.Context) {
	w.updateCount.Add(1)
	if w.onUpdate != nil {
		w.onUpdate()
	}
	w.ContentAvailable = true
}

func newTestAppWithFakeWidget(nextUpdate time.Time, html template.HTML) (*application, *fakeWidget, *page) {
	app := newTestApp()
	wgt := newFakeWidget(nextUpdate, html)
	p := &page{}
	app.widgetByID[wgt.GetID()] = wgt
	app.widgetToPage[wgt.GetID()] = p
	return app, wgt, p
}

func requestWidget(app *application, widgetID uint64) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/widgets/{id}/content/", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", widgetID))
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)
	return w
}

// --- edge-case tests ---

func TestWidgetContentInvalidIDFormats(t *testing.T) {
	app := newTestApp()
	cases := []struct {
		name string
		id   string
	}{
		{"alphabetic", "abc"},
		{"negative", "-1"},
		{"float", "1.5"},
		{"overflow", "18446744073709551616"},
		{"scientific", "1e2"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/widgets/{id}/content/", nil)
			req.SetPathValue("id", tc.id)
			w := httptest.NewRecorder()
			app.handleWidgetContentRequest(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("id %q: expected 404, got %d", tc.id, w.Code)
			}
		})
	}
}

func TestWidgetContentStaleWidget(t *testing.T) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(-time.Hour), "<p>stale</p>")

	w := requestWidget(app, wgt.GetID())

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if wgt.updateCount.Load() != 1 {
		t.Errorf("expected 1 update call, got %d", wgt.updateCount.Load())
	}
}

func TestWidgetContentFreshWidget(t *testing.T) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(time.Hour), "<p>fresh</p>")

	w := requestWidget(app, wgt.GetID())

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if wgt.updateCount.Load() != 0 {
		t.Errorf("expected 0 update calls for fresh widget, got %d", wgt.updateCount.Load())
	}
}

func TestWidgetContentForceRefresh(t *testing.T) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(time.Hour), "<p>forced</p>")

	req := httptest.NewRequest("GET", "/api/widgets/{id}/content/?force=true", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", wgt.GetID()))
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if wgt.updateCount.Load() != 1 {
		t.Errorf("expected 1 update call with force=true on fresh widget, got %d", wgt.updateCount.Load())
	}
}

func TestWidgetContentForceRefreshWithoutFlag(t *testing.T) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(time.Hour), "<p>no-force</p>")

	req := httptest.NewRequest("GET", "/api/widgets/{id}/content/", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", wgt.GetID()))
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)

	if wgt.updateCount.Load() != 0 {
		t.Errorf("expected 0 update calls without force flag, got %d", wgt.updateCount.Load())
	}
}

func TestWidgetContentInfiniteCache(t *testing.T) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Time{}, "<p>static</p>")
	wgt.cacheType = cacheTypeInfinite

	w := requestWidget(app, wgt.GetID())

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if wgt.updateCount.Load() != 0 {
		t.Errorf("expected 0 update calls for infinite-cache widget, got %d", wgt.updateCount.Load())
	}
	if wgt.NextUpdateUnixMilli() != 0 {
		t.Errorf("expected NextUpdateUnixMilli=0 for infinite cache, got %d", wgt.NextUpdateUnixMilli())
	}
}

func TestWidgetContentContentType(t *testing.T) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(time.Hour), "<p>hi</p>")

	w := requestWidget(app, wgt.GetID())

	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html; charset=utf-8, got %q", ct)
	}
}

// --- concurrency tests ---

func TestWidgetContentConcurrentSameWidget(t *testing.T) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(-time.Hour), "<p>concurrent</p>")
	wgt.onUpdate = func() {
		time.Sleep(10 * time.Millisecond)
	}

	const n = 50
	ready := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]int, n)

	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			w := requestWidget(app, wgt.GetID())
			results[i] = w.Code
		}()
	}

	close(ready)
	wg.Wait()

	for i, code := range results {
		if code != http.StatusOK {
			t.Errorf("goroutine %d: expected 200, got %d", i, code)
		}
	}
}

func TestWidgetContentConcurrentDifferentPages(t *testing.T) {
	app := newTestApp()
	const n = 10
	wgts := make([]*fakeWidget, n)

	for i := range n {
		wgt := newFakeWidget(time.Now().Add(-time.Hour), template.HTML(fmt.Sprintf("<p>widget-%d</p>", i)))
		p := &page{}
		app.widgetByID[wgt.GetID()] = wgt
		app.widgetToPage[wgt.GetID()] = p
		wgts[i] = wgt
	}

	ready := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]int, n)

	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			w := requestWidget(app, wgts[i].GetID())
			results[i] = w.Code
		}()
	}

	close(ready)
	wg.Wait()

	for i, code := range results {
		if code != http.StatusOK {
			t.Errorf("widget %d: expected 200, got %d", i, code)
		}
	}
}

func TestWidgetContentConcurrentMixedPages(t *testing.T) {
	app := newTestApp()
	pages := make([]*page, 3)
	for i := range pages {
		pages[i] = &page{}
	}

	const widgetsPerPage = 5
	var allWidgets []*fakeWidget
	for _, p := range pages {
		for j := range widgetsPerPage {
			wgt := newFakeWidget(time.Now().Add(-time.Hour), template.HTML(fmt.Sprintf("<p>w-%d</p>", j)))
			app.widgetByID[wgt.GetID()] = wgt
			app.widgetToPage[wgt.GetID()] = p
			allWidgets = append(allWidgets, wgt)
		}
	}

	ready := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]int, len(allWidgets))

	for i, wgt := range allWidgets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			w := requestWidget(app, wgt.GetID())
			results[i] = w.Code
		}()
	}

	close(ready)
	wg.Wait()

	for i, code := range results {
		if code != http.StatusOK {
			t.Errorf("widget %d: expected 200, got %d", i, code)
		}
	}
}

// --- stress test ---

func TestWidgetContentStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	app := newTestApp()
	const numPages = 10
	const widgetsPerPage = 10
	var ids []uint64

	for i := range numPages {
		p := &page{}
		for j := range widgetsPerPage {
			wgt := newFakeWidget(
				time.Now().Add(-time.Duration(i*widgetsPerPage+j)*time.Second),
				template.HTML(fmt.Sprintf("<p>stress-%d-%d</p>", i, j)),
			)
			app.widgetByID[wgt.GetID()] = wgt
			app.widgetToPage[wgt.GetID()] = p
			ids = append(ids, wgt.GetID())
		}
	}

	const goroutines = 100
	const requestsPerGoroutine = 100
	var failures atomic.Int32
	var wg sync.WaitGroup

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range requestsPerGoroutine {
				id := ids[rand.IntN(len(ids))]
				w := requestWidget(app, id)
				if w.Code != http.StatusOK {
					failures.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	if f := failures.Load(); f > 0 {
		t.Errorf("%d/%d requests failed", f, goroutines*requestsPerGoroutine)
	}
}

// --- benchmarks ---

func BenchmarkWidgetContentCached(b *testing.B) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(24*time.Hour), "<p>cached</p>")
	id := wgt.GetID()
	b.ResetTimer()

	for range b.N {
		requestWidget(app, id)
	}
}

func BenchmarkWidgetContentStale(b *testing.B) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(-time.Hour), "<p>stale</p>")
	wgt.onUpdate = func() {
		wgt.nextUpdate = time.Now().Add(-time.Hour)
	}
	id := wgt.GetID()
	b.ResetTimer()

	for range b.N {
		requestWidget(app, id)
	}
}

func BenchmarkWidgetContentParallel(b *testing.B) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(24*time.Hour), "<p>parallel</p>")
	id := wgt.GetID()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			requestWidget(app, id)
		}
	})
}

func BenchmarkWidgetContentManyWidgets(b *testing.B) {
	app := newTestApp()
	const n = 1000
	ids := make([]uint64, n)

	for i := range n {
		wgt := newFakeWidget(time.Now().Add(24*time.Hour), template.HTML(fmt.Sprintf("<p>w-%d</p>", i)))
		p := &page{}
		app.widgetByID[wgt.GetID()] = wgt
		app.widgetToPage[wgt.GetID()] = p
		ids[i] = wgt.GetID()
	}
	b.ResetTimer()

	for i := range b.N {
		requestWidget(app, ids[i%n])
	}
}

func BenchmarkWidgetContentParallelDifferentPages(b *testing.B) {
	app := newTestApp()
	const n = 10
	ids := make([]uint64, n)

	for i := range n {
		wgt := newFakeWidget(time.Now().Add(24*time.Hour), template.HTML(fmt.Sprintf("<p>page-%d</p>", i)))
		p := &page{}
		app.widgetByID[wgt.GetID()] = wgt
		app.widgetToPage[wgt.GetID()] = p
		ids[i] = wgt.GetID()
	}
	b.ResetTimer()

	var idx atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := idx.Add(1)
			requestWidget(app, ids[i%n])
		}
	})
}
