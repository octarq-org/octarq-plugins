package links

import (
	"net/http"
	"testing"
)

func TestRoleGateFailsClosedWithoutSeam(t *testing.T) {
	p := &Plugin{}
	if p.hasRole(&http.Request{}, "admin") {
		t.Fatal("role gate passed with no RequireRole wired; an unwired host must be refused, not trusted")
	}
}

func TestRoleGateDelegatesWhenWired(t *testing.T) {
	var askedFor string
	p := &Plugin{requireRole: func(_ *http.Request, min string) bool {
		askedFor = min
		return min == "admin"
	}}
	if !p.hasRole(&http.Request{}, "admin") {
		t.Fatal("role gate refused an admin caller")
	}
	if askedFor != "admin" {
		t.Fatalf("gate asked for role %q, want admin", askedFor)
	}
	if p.hasRole(&http.Request{}, "owner") {
		t.Fatal("role gate granted owner to an admin-only caller")
	}
}
