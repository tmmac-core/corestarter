package minercore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// The CTR API client follows no redirect to another host.
func TestAPIClientRefusesForeignRedirect(t *testing.T) {
	var hit atomic.Bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		w.Write([]byte("{}"))
	}))
	defer foreign.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/api/stats", http.StatusFound)
	}))
	defer api.Close()

	_, err := httpGet(api.URL + "/api/stats")
	var he HTTPError
	if !errors.As(err, &he) || he.Code != http.StatusFound {
		t.Errorf("redirect: got %v, want HTTPError 302", err)
	}
	if hit.Load() {
		t.Error("followed a redirect to a foreign host")
	}
}

func TestCTRRedirectOnly(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://de-api.catchthatrabbit.com/api/stats": true,
		"https://catchthatrabbit.com/":                 true,
		"https://FI-API.CatchThatRabbit.com/x":         true,
		"https://de-api.catchthatrabbit.com:8443/x":    true,
		"http://de-api.catchthatrabbit.com/api/stats":  false,
		"https://evilcatchthatrabbit.com/":             false,
		"https://catchthatrabbit.com.example.org/":     false,
		"https://example.org/?catchthatrabbit.com":     false,
		"https://127.0.0.1/":                           false,
		"ftp://de-api.catchthatrabbit.com/":            false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		err = ctrRedirectOnly(&http.Request{URL: u}, []*http.Request{{}})
		if got := err == nil; got != ok {
			t.Errorf("%s: allowed %v, want %v (err %v)", raw, got, ok, err)
		}
	}
	u, _ := url.Parse("https://de-api.catchthatrabbit.com/")
	if ctrRedirectOnly(&http.Request{URL: u}, make([]*http.Request, 10)) == nil {
		t.Error("no limit on redirects")
	}
}
