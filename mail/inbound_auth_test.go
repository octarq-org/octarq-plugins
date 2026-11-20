package mail

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type recordedAudit struct {
	action   string
	targetID uint
	meta     map[string]any
}

var nextInboundAuthOrgID atomic.Uint32

func inboundAuthFixture(t *testing.T) (*Plugin, *[]recordedAudit, string, uint) {
	t.Helper()
	db := setupTestDB(t)
	slug := "inbound-auth-" + strings.ToLower(t.Name())
	org := testOrg{Name: t.Name(), Slug: slug, InboundToken: "right-token"}
	org.ID = 9000 + uint(nextInboundAuthOrgID.Add(1))
	if err := db.Create(&org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}

	var seen []recordedAudit
	p := &Plugin{db: db}
	p.audit = func(_ *http.Request, action, _ string, targetID uint, meta map[string]any) {
		seen = append(seen, recordedAudit{action: action, targetID: targetID, meta: meta})
	}
	return p, &seen, slug, org.ID
}

func genericInput(slug, token string) *InboundGenericInput {
	body := []byte("From: a@example.com\r\nTo: bob@example.test\r\nSubject: T\r\n\r\nhi")
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/"+slug+"/email/inbound/raw/"+token, bytes.NewReader(body))
	return &InboundGenericInput{
		Ctx:     humago.NewContext(nil, req, httptest.NewRecorder()),
		OrgSlug: slug,
		Token:   token,
	}
}

func TestGenericInboundRejectsHeaderToken(t *testing.T) {
	p, _, slug, _ := inboundAuthFixture(t)

	body := []byte("From: a@example.com\r\nTo: bob@example.test\r\n\r\nhi")
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/"+slug+"/email/inbound/raw/", bytes.NewReader(body))
	req.Header.Set("X-Octarq-Token", "right-token")
	req.Header.Set("Authorization", "Bearer right-token")

	_, err := p.inboundGeneric(context.Background(), &InboundGenericInput{
		Ctx:     humago.NewContext(nil, req, httptest.NewRecorder()),
		OrgSlug: slug,
		Token:   "",
	})
	if err == nil {
		t.Fatal("a correct token in a header must not authenticate — the path segment is the credential")
		return
	}
}

func TestGenericInboundRejectsWrongPathToken(t *testing.T) {
	p, _, slug, _ := inboundAuthFixture(t)
	if _, err := p.inboundGeneric(context.Background(), genericInput(slug, "wrong-token")); err == nil {
		t.Fatal("wrong path token must be rejected")
		return
	}
}

func TestInboundAuthFailureIsAudited(t *testing.T) {
	p, seen, slug, orgID := inboundAuthFixture(t)

	if _, err := p.inboundGeneric(context.Background(), genericInput(slug, "wrong-token")); err == nil {
		t.Fatal("expected rejection")
		return
	}

	if len(*seen) != 1 {
		t.Fatalf("expected exactly one audit entry for a rejected token, got %d", len(*seen))
	}
	got := (*seen)[0]
	if got.action != "email.inbound.auth_failed" {
		t.Errorf("audit action = %q, want email.inbound.auth_failed", got.action)
	}
	if got.targetID != orgID {
		t.Errorf("audit targetID = %d, want the org id", got.targetID)
	}

	for k, v := range got.meta {
		if s, ok := v.(string); ok && s == "wrong-token" {
			t.Errorf("audit meta[%q] contains the attempted token — it must never be stored", k)
		}
	}
}

func TestPathTokenRouteAuthFailureIsAudited(t *testing.T) {
	p, seen, slug, _ := inboundAuthFixture(t)

	body := []byte("From: a@example.com\r\nTo: bob@example.test\r\n\r\nhi")
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/"+slug+"/email/inbound/nope", bytes.NewReader(body))
	_, err := p.inbound(context.Background(), &InboundInput{
		Ctx:     humago.NewContext(nil, req, httptest.NewRecorder()),
		OrgSlug: slug,
		Token:   "nope",
	})
	if err == nil {
		t.Fatal("expected rejection")
		return
	}
	if len(*seen) != 1 || (*seen)[0].action != "email.inbound.auth_failed" {
		t.Fatalf("expected an auth-failure audit entry, got %+v", *seen)
	}
}
