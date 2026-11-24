package mail

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/octarq-org/octarq/server/pkg/hostlist"
	"github.com/octarq-org/octarq/server/pkg/safego"
)

type domainMailHost struct {
	OrgID     uint              `gorm:"column:owner_id"`
	Name      string            `gorm:"column:name"`
	MailHosts hostlist.HostList `gorm:"column:mail_hosts"`
}

func reporterIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		if idx := strings.IndexByte(ip, ','); idx >= 0 {
			return strings.TrimSpace(ip[:idx])
		}
		return strings.TrimSpace(ip)
	}
	return r.RemoteAddr
}

func (p *Plugin) isReservedMailbox(orgID uint, addr string) bool {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 {
		return false
	}
	user := strings.ToLower(parts[0])
	reserved := []string{"admin", "administrator", "hostmaster", "postmaster", "webmaster"}
	for _, r := range reserved {
		if user == r {
			return true
		}
	}
	return false
}

type InboundEmail struct {
	ID         uint      `json:"id"`
	MailboxID  uint      `json:"mailboxId"`
	OrgID      uint      `json:"orgId"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Subject    string    `json:"subject"`
	Text       string    `json:"text"`
	HTML       string    `json:"html"`
	ReceivedAt time.Time `json:"receivedAt"`
}

func (p *Plugin) emitEmail(e InboundEmail) {
	payload, err := json.Marshal(e)
	if err != nil {
		return
	}
	p.emailMu.RLock()
	handlers := p.emailHandlers
	p.emailMu.RUnlock()
	for _, h := range handlers {
		if h != nil {
			handler := h
			safego.Go("mail.dispatch-plugin", func() {
				handler("mail.inbound", payload)
			})
		}
	}
}

func (p *Plugin) Dispatch(handler func(topic string, payload json.RawMessage)) {
	if handler == nil {
		return
	}
	p.emailMu.Lock()
	defer p.emailMu.Unlock()
	p.emailHandlers = append(p.emailHandlers, handler)
}

func (p *Plugin) mailHostDisabled(orgID uint, host string) bool {
	if orgID == 0 {
		return false
	}
	normHost := hostlist.NormalizeHost(host)
	var doms []domainMailHost
	p.db.Table("domains").Where("owner_id = ? AND for_mail = ?", orgID, true).Find(&doms)
	listed := false
	for _, d := range doms {
		for _, mh := range d.MailHosts {
			if hostlist.NormalizeHost(mh.Host) == normHost {
				listed = true
				if mh.Enabled {
					return false
				}
			}
		}
	}
	return listed
}

func (p *Plugin) mailAddressDomainNotAnotherTenants(orgID uint, addr string) bool {
	if orgID == 0 || p.db == nil {
		return false
	}
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return false
	}
	normHost := hostlist.NormalizeHost(addr[at+1:])
	if normHost == "" {
		return false
	}
	var doms []domainMailHost
	if err := p.db.Table("domains").Where("for_mail = ?", true).Find(&doms).Error; err != nil {
		return false
	}
	for _, d := range doms {
		if d.OrgID == orgID {
			continue
		}
		for _, mh := range d.MailHosts {
			if hostlist.NormalizeHost(mh.Host) == normHost {
				return false
			}
		}
	}
	return true
}

func (p *Plugin) resolveMailbox(orgID uint, addr string) (*Mailbox, bool) {
	if addr == "" {
		return nil, false
	}
	if at := strings.LastIndex(addr, "@"); at >= 0 && p.mailHostDisabled(orgID, addr[at+1:]) {
		return nil, false
	}
	var mb Mailbox
	if err := p.db.Where("address = ? AND enabled = ? AND owner_id = ?", addr, true, orgID).First(&mb).Error; err == nil {
		return &mb, true
	}
	if p.getWorkspaceSetting(orgID, "catch_all") != "true" {
		return nil, false
	}
	if p.isReservedMailbox(orgID, addr) {
		return nil, false
	}
	if !p.ownsMailHost(orgID, addr) {
		return nil, false
	}
	mb = Mailbox{OrgID: orgID, Address: addr, Enabled: true, Note: "auto (catch-all)"}
	if err := p.db.Create(&mb).Error; err != nil {
		return nil, false
	}
	return &mb, true
}

func (p *Plugin) ownsMailHost(orgID uint, addr string) bool {
	if orgID == 0 || p.db == nil {
		return false
	}
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return false
	}
	normHost := hostlist.NormalizeHost(addr[at+1:])
	if normHost == "" {
		return false
	}
	var doms []domainMailHost
	if err := p.db.Table("domains").Where("owner_id = ? AND for_mail = ?", orgID, true).Find(&doms).Error; err != nil {
		return false
	}
	for _, d := range doms {
		hosts := d.MailHosts.Enabled()
		if len(hosts) == 0 {
			if hostlist.NormalizeHost(d.Name) == normHost {
				return true
			}
			continue
		}
		for _, mh := range hosts {
			if hostlist.NormalizeHost(mh) == normHost {
				return true
			}
		}
	}
	return false
}
