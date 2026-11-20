package links

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPageMetaBlocksInternal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<title>internal</title>"))
	}))
	defer srv.Close()

	title, desc := fetchPageMeta(context.Background(), srv.URL)
	if title != "" || desc != "" {
		t.Errorf("fetchPageMeta fetched an internal URL: title=%q desc=%q", title, desc)
	}
}
