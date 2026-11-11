package glance

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func renderCustomAPIForTest(t *testing.T, primary *CustomAPIRequest, subs map[string]*CustomAPIRequest, tmpl string) (string, error) {
	t.Helper()

	if err := primary.initialize(); err != nil {
		t.Fatalf("initializing primary request: %v", err)
	}
	for key, req := range subs {
		if err := req.initialize(); err != nil {
			t.Fatalf("initializing subrequest %q: %v", key, err)
		}
	}

	compiled := template.Must(template.New("").Funcs(customAPITemplateFuncs).Parse(tmpl))
	html, err := fetchAndRenderCustomAPIRequest(primary, subs, nil, compiled)

	return strings.TrimSpace(string(html)), err
}

func TestCustomAPIAllowFailure(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"up"}`))
	}))
	defer ok.Close()

	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "404 page not found", http.StatusNotFound)
	}))
	defer notFound.Close()

	// A server that is gone: connection refused, like a machine that is asleep
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	const tmpl = `{{ .Failed }} {{ .JSON.String "name" }} | {{ $s := .Subrequest "sub" }}{{ $s.Failed }} {{ $s.JSON.String "name" }}`

	tests := []struct {
		name    string
		primary *CustomAPIRequest
		sub     *CustomAPIRequest
		want    string
		wantErr bool
	}{
		{
			name:    "all requests succeed",
			primary: &CustomAPIRequest{URL: ok.URL},
			sub:     &CustomAPIRequest{URL: ok.URL},
			want:    "false up | false up",
		},
		{
			name:    "unreachable primary with allow-failure renders the rest",
			primary: &CustomAPIRequest{URL: goneURL, AllowFailure: true},
			sub:     &CustomAPIRequest{URL: ok.URL},
			want:    "true  | false up",
		},
		{
			name:    "non-JSON error page in a subrequest with allow-failure",
			primary: &CustomAPIRequest{URL: ok.URL},
			sub:     &CustomAPIRequest{URL: notFound.URL, AllowFailure: true},
			want:    "false up | true",
		},
		{
			name:    "failing subrequest without allow-failure fails the widget",
			primary: &CustomAPIRequest{URL: ok.URL},
			sub:     &CustomAPIRequest{URL: goneURL},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderCustomAPIForTest(t, tt.primary, map[string]*CustomAPIRequest{"sub": tt.sub}, tmpl)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCustomAPIAllowFailureWithoutSubrequests(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	got, err := renderCustomAPIForTest(t, &CustomAPIRequest{URL: goneURL, AllowFailure: true}, nil,
		`{{ if .Failed }}asleep{{ else }}awake{{ end }}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "asleep" {
		t.Errorf("got %q, want %q", got, "asleep")
	}
}
