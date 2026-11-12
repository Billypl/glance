package glance

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestApp() *application {
	return &application{
		slugToPage:   make(map[string]*page),
		widgetByID:   make(map[uint64]widget),
		widgetToPage: make(map[uint64]*page),
	}
}

func TestWidgetContentEndpoint401(t *testing.T) {
	app := newTestApp()
	app.RequiresAuth = true

	req := httptest.NewRequest("GET", "/api/widgets/1/content/", nil)
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWidgetContentEndpoint404(t *testing.T) {
	app := newTestApp()

	req := httptest.NewRequest("GET", "/api/widgets/9999/content/", nil)
	req.SetPathValue("id", "9999")
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestWidgetContentEndpoint200(t *testing.T) {
	app := newTestApp()

	wgt := &monitorWidget{}
	wgt.setID(widgetIDCounter.Add(1))
	wgt.withError(nil)
	p := &page{}

	app.widgetByID[wgt.GetID()] = wgt
	app.widgetToPage[wgt.GetID()] = p

	req := httptest.NewRequest("GET", "/api/widgets/{id}/content/", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", wgt.GetID()))
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body == "" {
		t.Error("expected non-empty HTML body")
	}
}

func TestWidgetContentEndpointNestedWidget(t *testing.T) {
	app := newTestApp()

	child := &monitorWidget{}
	child.setID(widgetIDCounter.Add(1))
	child.withError(nil)

	parent := &groupWidget{}
	parent.setID(widgetIDCounter.Add(1))
	parent.containerWidgetBase.Widgets = widgets{child}

	p := &page{}
	registerWidget(app.widgetByID, app.widgetToPage, parent, p)

	req := httptest.NewRequest("GET", "/api/widgets/{id}/content/", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", child.GetID()))
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for nested widget, got %d", w.Code)
	}
}

func TestWidgetContentResponseContainsDataWidgetID(t *testing.T) {
	app := newTestApp()

	wgt := &monitorWidget{}
	wgt.setID(widgetIDCounter.Add(1))
	wgt.withError(nil)
	p := &page{}

	app.widgetByID[wgt.GetID()] = wgt
	app.widgetToPage[wgt.GetID()] = p

	req := httptest.NewRequest("GET", "/api/widgets/{id}/content/", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", wgt.GetID()))
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)

	body := w.Body.String()
	expected := fmt.Sprintf(`data-widget-id="%d"`, wgt.GetID())
	if !strings.Contains(body, expected) {
		t.Errorf("response missing %q; body: %s", expected, body)
	}
}

func TestWidgetContentConsecutiveFetchesAreStructurallyConsistent(t *testing.T) {
	app := newTestApp()

	wgt := &monitorWidget{}
	wgt.setID(widgetIDCounter.Add(1))
	wgt.withError(nil)
	p := &page{}

	app.widgetByID[wgt.GetID()] = wgt
	app.widgetToPage[wgt.GetID()] = p

	bodies := make([]string, 3)
	for i := range bodies {
		req := httptest.NewRequest("GET", "/api/widgets/{id}/content/", nil)
		req.SetPathValue("id", fmt.Sprintf("%d", wgt.GetID()))
		w := httptest.NewRecorder()
		app.handleWidgetContentRequest(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("fetch %d: expected 200, got %d", i, w.Code)
		}
		bodies[i] = w.Body.String()
	}

	expectedID := fmt.Sprintf(`data-widget-id="%d"`, wgt.GetID())
	for i, body := range bodies {
		if !strings.Contains(body, expectedID) {
			t.Errorf("fetch %d: missing %s", i, expectedID)
		}
		if !strings.Contains(body, "data-next-update=") {
			t.Errorf("fetch %d: missing data-next-update", i)
		}
	}

	rootTag := func(html string) string {
		idx := strings.Index(html, "<")
		if idx == -1 {
			return ""
		}
		end := strings.IndexAny(html[idx+1:], " \t\n>")
		if end == -1 {
			return ""
		}
		return html[idx+1 : idx+1+end]
	}

	tag0 := rootTag(bodies[0])
	for i := 1; i < len(bodies); i++ {
		if tagI := rootTag(bodies[i]); tagI != tag0 {
			t.Errorf("fetch %d root tag %q differs from fetch 0 root tag %q", i, tagI, tag0)
		}
	}
}

func TestWidgetContentResponseContainsDataNextUpdate(t *testing.T) {
	app := newTestApp()

	wgt := &monitorWidget{}
	wgt.setID(widgetIDCounter.Add(1))
	wgt.withError(nil)
	p := &page{}

	app.widgetByID[wgt.GetID()] = wgt
	app.widgetToPage[wgt.GetID()] = p

	req := httptest.NewRequest("GET", "/api/widgets/{id}/content/", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", wgt.GetID()))
	w := httptest.NewRecorder()
	app.handleWidgetContentRequest(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "data-next-update=") {
		t.Errorf("response missing data-next-update attribute; body: %s", body)
	}
}

func TestUpdateInMilliIsRelativeToRendering(t *testing.T) {
	wgt := newFakeWidget(time.Now().Add(30*time.Second), "")
	if in := wgt.UpdateInMilli(); in < 29_000 || in > 30_000 {
		t.Errorf("UpdateInMilli = %d, want about 30000", in)
	}

	wgt.nextUpdate = time.Now().Add(-time.Second)
	if in := wgt.UpdateInMilli(); in != 0 {
		t.Errorf("UpdateInMilli for a past update = %d, want 0", in)
	}

	wgt.cacheType = cacheTypeInfinite
	if in := wgt.UpdateInMilli(); in != 0 {
		t.Errorf("UpdateInMilli for infinite cache = %d, want 0", in)
	}
}

func TestWidgetContentResponseContainsDataUpdateIn(t *testing.T) {
	app, wgt, _ := newTestAppWithFakeWidget(time.Now().Add(time.Minute), "")
	body := requestWidget(app, wgt.GetID()).Body.String()
	if !strings.Contains(body, `data-update-in="`) {
		t.Errorf("response missing data-update-in attribute; body: %s", body)
	}
}

func TestUserAssetsAreRevalidated(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "custom.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := http.StripPrefix("/assets/", fileServerWithCacheControl(http.Dir(dir), "no-cache"))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/assets/custom.js", nil))
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	lastModified := w.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("missing Last-Modified, browsers could not revalidate")
	}

	req := httptest.NewRequest("GET", "/assets/custom.js", nil)
	req.Header.Set("If-Modified-Since", lastModified)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotModified {
		t.Errorf("unchanged file: status %d, want 304", w.Code)
	}
}
