package mail

import (
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

const (
	maxPartBytes = 10 << 20
	maxParts     = 200
)

func readLimited(r io.Reader) (b []byte, truncated bool, totalBytes int) {
	var readErr error
	b, readErr = io.ReadAll(io.LimitReader(r, maxPartBytes))
	if readErr != nil {
		slog.Warn("mail: read limited part error", "error", readErr)
	}
	nDiscarded, copyErr := io.Copy(io.Discard, r)
	if copyErr != nil {
		slog.Warn("mail: discard remaining part error", "error", copyErr)
	}
	totalBytes = len(b) + int(nDiscarded)
	truncated = nDiscarded > 0
	return b, truncated, totalBytes
}

func init() {
	message.CharsetReader = charset.Reader
}

type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Inline      bool   `json:"inline,omitempty"`
	ContentID   string `json:"contentId,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

type AuthResults struct {
	SPF   string `json:"spf"`
	DKIM  string `json:"dkim"`
	DMARC string `json:"dmarc"`
}

type Parsed struct {
	MessageID       string
	From            string
	To              string
	Subject         string
	Text            string
	HTML            string
	Attachments     []Attachment
	ReceivedAt      time.Time
	Raw             []byte
	Auth            AuthResults
	PartErrors      int
	MaxPartsReached bool
}

func Parse(raw []byte) (*Parsed, error) {
	mr, err := mail.CreateReader(strings.NewReader(string(raw)))
	if err != nil {
		return &Parsed{Raw: raw, ReceivedAt: time.Now(), Subject: "(unparseable message)"}, nil
	}
	h := mr.Header
	p := &Parsed{Raw: raw, ReceivedAt: time.Now()}
	var msgIDErr error
	p.MessageID, msgIDErr = h.MessageID()
	if msgIDErr != nil {
		slog.Debug("mail: header Message-ID missing or invalid", "error", msgIDErr)
	}
	var subjErr error
	p.Subject, subjErr = h.Subject()
	if subjErr != nil {
		slog.Debug("mail: header Subject missing or invalid", "error", subjErr)
	}
	if addrs, err := h.AddressList("From"); err == nil && len(addrs) > 0 {
		p.From = addrs[0].Address
	}
	if addrs, err := h.AddressList("To"); err == nil && len(addrs) > 0 {
		p.To = addrs[0].Address
	}
	if t, err := h.Date(); err == nil && !t.IsZero() {
		p.ReceivedAt = t
	}
	for _, v := range h.Values("Authentication-Results") {
		parseAuthResults(v, &p.Auth)
	}

	parts := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			p.PartErrors++
			slog.Warn("failed to parse mime part", "message_id", p.MessageID, "error", err)
			if p.PartErrors > maxParts {
				break
			}
			continue
		}
		if parts++; parts > maxParts {
			p.PartErrors++
			p.MaxPartsReached = true
			slog.Warn("max mime parts limit reached", "message_id", p.MessageID, "limit", maxParts)
			break
		}
		switch hdr := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := hdr.ContentType()
			b, truncated, totalBytes := readLimited(part.Body)
			if strings.HasPrefix(ct, "text/html") {
				if p.HTML == "" && len(b) > 0 {
					p.HTML = string(b)
				} else if len(b) > 0 {
					p.PartErrors++
					slog.Warn("duplicate text/html part ignored", "message_id", p.MessageID)
				}
			} else if strings.HasPrefix(ct, "text/plain") {
				if p.Text == "" && len(b) > 0 {
					p.Text = string(b)
				} else if len(b) > 0 {
					p.PartErrors++
					slog.Warn("duplicate text/plain part ignored", "message_id", p.MessageID)
				}
			} else {
				filename := ""
				if _, params, err := hdr.ContentType(); err == nil {
					filename = params["name"]
				}
				if filename == "" {
					if cd := hdr.Get("Content-Disposition"); cd != "" {
						for _, part := range strings.Split(cd, ";") {
							part = strings.TrimSpace(part)
							if strings.HasPrefix(strings.ToLower(part), "filename=") {
								filename = strings.Trim(part[9:], "\"")
								break
							}
						}
					}
				}
				cid := strings.Trim(hdr.Get("Content-ID"), "<>")
				p.Attachments = append(p.Attachments, Attachment{
					Filename:    filename,
					ContentType: ct,
					Size:        totalBytes,
					Inline:      true,
					ContentID:   cid,
					Truncated:   truncated,
				})
			}
		case *mail.AttachmentHeader:
			ct, _, _ := hdr.ContentType()
			filename, _ := hdr.Filename()
			_, truncated, totalBytes := readLimited(part.Body)
			cid := strings.Trim(hdr.Get("Content-ID"), "<>")
			p.Attachments = append(p.Attachments, Attachment{
				Filename:    filename,
				ContentType: ct,
				Size:        totalBytes,
				Inline:      false,
				ContentID:   cid,
				Truncated:   truncated,
			})
		}
	}
	return p, nil
}

func parseAuthResults(hdr string, out *AuthResults) {
	s := strings.ToLower(hdr)
	for _, method := range []struct {
		name   string
		target *string
	}{
		{"spf=", &out.SPF},
		{"dkim=", &out.DKIM},
		{"dmarc=", &out.DMARC},
	} {
		idx := 0
		for {
			pos := strings.Index(s[idx:], method.name)
			if pos < 0 {
				break
			}
			matchPos := idx + pos
			idx = matchPos + len(method.name)

			if matchPos > 0 {
				prev := s[matchPos-1]
				if prev != ' ' && prev != '\t' && prev != ';' && prev != ',' {
					continue
				}
			}

			rest := s[matchPos+len(method.name):]
			end := strings.IndexAny(rest, " \t\r\n;(")
			if end < 0 {
				end = len(rest)
			}
			token := strings.TrimSpace(rest[:end])
			if token != "" && *method.target == "" {
				*method.target = token
			}
			break
		}
	}
}
