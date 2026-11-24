package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/octarq-org/octarq/server/plugin"
)

var (
	mcpResourcesMu sync.Mutex
	mcpResources   = make(map[string]string)
)

func hasTemplateOnServer(srv *mcp.Server, uriTemplate string) bool {
	if srv == nil {
		return false
	}
	v := reflect.ValueOf(srv)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return false
	}
	rf := v.FieldByName("resourceTemplates")
	if !rf.IsValid() {
		return false
	}
	if rf.Kind() == reflect.Ptr {
		rf = rf.Elem()
	}
	if !rf.IsValid() || rf.Kind() != reflect.Struct {
		return false
	}
	ff := rf.FieldByName("features")
	if !ff.IsValid() || ff.Kind() != reflect.Map {
		return false
	}
	return ff.MapIndex(reflect.ValueOf(uriTemplate)).IsValid()
}

func addMCPResourceTemplate(srv *mcp.Server, owner string, t *mcp.ResourceTemplate, h mcp.ResourceHandler) {
	mcpResourcesMu.Lock()
	defer mcpResourcesMu.Unlock()

	if existing, exists := mcpResources[t.URITemplate]; exists && existing != owner {
		panic(fmt.Sprintf("mcp: resource template conflict: %q already registered by %q", t.URITemplate, existing))
	}
	if hasTemplateOnServer(srv, t.URITemplate) && mcpResources[t.URITemplate] != owner {
		panic(fmt.Sprintf("mcp: resource template conflict: %q already registered on server", t.URITemplate))
	}
	mcpResources[t.URITemplate] = owner
	srv.AddResourceTemplate(t, h)
}

type MailboxLatestResource struct {
	OrgID       uint                `json:"org_id"`
	MailboxID   uint                `json:"mailbox_id"`
	Address     string              `json:"address"`
	Unread      int64               `json:"unread"`
	Latest      *MailboxLatestEmail `json:"latest"`
	GeneratedAt time.Time           `json:"generated_at"`
}

type MailboxLatestEmail struct {
	ID         uint      `json:"id"`
	From       string    `json:"from"`
	Subject    string    `json:"subject"`
	ReceivedAt time.Time `json:"received_at"`
}

func parseMailboxLatestURI(uri string) (uint, bool) {
	const prefix = "octarq://mailboxes/"
	const suffix = "/latest"
	if !strings.HasPrefix(uri, prefix) || !strings.HasSuffix(uri, suffix) {
		return 0, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(uri, prefix), suffix)
	if raw == "" || strings.Contains(raw, "/") {
		return 0, false
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

func (p *Plugin) registerMCPResources(srv *mcp.Server) {
	addMCPResourceTemplate(srv, "mail", &mcp.ResourceTemplate{
		URITemplate: "octarq://mailboxes/{id}/latest",
		Name:        "mailbox_latest",
		Title:       "Mailbox Latest",
		Description: "Newest message metadata of one mailbox (no bodies).",
		MIMEType:    "application/json",
	}, p.mcpResourceMailboxLatest)
}

func (p *Plugin) mcpResourceMailboxLatest(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if req == nil || req.Params == nil {
		return nil, errors.New("missing request params")
	}
	mailboxID, ok := parseMailboxLatestURI(req.Params.URI)
	if !ok {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	orgID := plugin.OrgIDFromContext(ctx)
	if orgID == 0 {
		return nil, errNoOrgInContext
	}
	if p.db == nil {
		return nil, errors.New("mail database unavailable")
	}
	var mb Mailbox
	if err := p.db.WithContext(ctx).
		Where("id = ? AND owner_id = ?", mailboxID, orgID).
		First(&mb).Error; err != nil {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	var unread int64
	if err := p.db.WithContext(ctx).Model(&Email{}).
		Where("mailbox_id = ? AND read = ?", mailboxID, false).
		Count(&unread).Error; err != nil {
		return nil, err
	}
	var latestEmail Email
	var latestOut *MailboxLatestEmail
	if err := p.db.WithContext(ctx).Model(&Email{}).
		Where("mailbox_id = ?", mailboxID).
		Order("received_at DESC, id DESC").
		First(&latestEmail).Error; err == nil {
		latestOut = &MailboxLatestEmail{
			ID:         latestEmail.ID,
			From:       latestEmail.FromAddr,
			Subject:    latestEmail.Subject,
			ReceivedAt: latestEmail.ReceivedAt,
		}
	}
	res := MailboxLatestResource{
		OrgID:       orgID,
		MailboxID:   mailboxID,
		Address:     mb.Address,
		Unread:      unread,
		Latest:      latestOut,
		GeneratedAt: time.Now().UTC(),
	}
	buf, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(buf),
			},
		},
	}, nil
}
