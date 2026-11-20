package mail

import (
	"html"
	"regexp"
	"strings"
)

const (
	MaxEmailContentBytes = 4096
)

var (
	scriptStyleRegex = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)

	htmlTagRegex = regexp.MustCompile(`<[^>]+>`)

	dataURIRegex = regexp.MustCompile(`data:image/[^;]+;base64,[A-Za-z0-9+/=]+`)

	rawBase64Regex = regexp.MustCompile(`\b[A-Za-z0-9+/=]{120,}\b`)

	sensitiveQueryTokenRegex = regexp.MustCompile(`(?i)(token|reset_token|auth_token|magic_token|access_token|verification_token|password_reset|secret|key|ticket)=([a-zA-Z0-9_\-\.]{12,})`)

	sensitivePathTokenRegex = regexp.MustCompile(`(?i)(/(?:reset[-_]password|password[-_]reset|verify[-_]email|magic[-_]link|confirm[-_]email)/)([a-zA-Z0-9_\-\.]{12,})`)

	multipleNewlinesRegex = regexp.MustCompile(`\n{3,}`)
)

func SanitizeEmailContent(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}

	content := raw

	content = dataURIRegex.ReplaceAllString(content, "[inline image truncated]")

	content = rawBase64Regex.ReplaceAllString(content, "[base64 data truncated]")

	content = sensitiveQueryTokenRegex.ReplaceAllString(content, "$1=[REDACTED_TOKEN]")
	content = sensitivePathTokenRegex.ReplaceAllString(content, "${1}[REDACTED_TOKEN]")

	truncated := false
	if len(content) > MaxEmailContentBytes {
		content = content[:MaxEmailContentBytes] + "\n... [content truncated at 4KB]"
		truncated = true
	}

	return content, truncated
}

func stripHTML(h string) string {
	if h == "" {
		return ""
	}

	s := scriptStyleRegex.ReplaceAllString(h, "")

	blockTags := []string{"</p>", "<p>", "<br>", "<br/>", "<br />", "</div>", "<div>", "</tr>", "</li>", "</h1>", "</h2>", "</h3>"}
	for _, bt := range blockTags {
		s = strings.ReplaceAll(s, bt, "\n")
	}

	s = htmlTagRegex.ReplaceAllString(s, "")

	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")

	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = multipleNewlinesRegex.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

func GenerateEmailSummary(subject, body, otp string) (string, string) {
	category := "general"
	lowerSubj := strings.ToLower(subject)
	lowerBody := strings.ToLower(body)
	full := lowerSubj + " " + lowerBody

	if otp != "" || hasOTPContext(full) {
		category = "otp"
	} else if strings.Contains(full, "security") || strings.Contains(full, "password") || strings.Contains(full, "alert") {
		category = "security_alert"
	} else if strings.Contains(full, "invoice") || strings.Contains(full, "bill") || strings.Contains(full, "receipt") || strings.Contains(full, "payment") {
		category = "billing"
	} else if strings.Contains(full, "marketing") || strings.Contains(full, "discount") || strings.Contains(full, "newsletter") || strings.Contains(full, "promo") || strings.Contains(full, "special offer") {
		category = "marketing"
	} else if strings.Contains(full, "notification") || strings.Contains(full, "update") {
		category = "notification"
	}

	cleanBody, _ := SanitizeEmailContent(body)
	cleanBody = strings.TrimSpace(cleanBody)
	cleanBody = strings.Join(strings.Fields(cleanBody), " ")

	runes := []rune(cleanBody)
	summaryText := cleanBody
	if len(runes) > 280 {
		summaryText = string(runes[:280]) + "..."
	}

	if summaryText == "" {
		summaryText = subject
	}

	return summaryText, category
}

func NormalizeCategory(cat string) string {
	switch cat {
	case "billing":
		return "bill"
	case "security_alert":
		return "important"
	default:
		return cat
	}
}

func CalculateImportance(cat string) int {
	switch NormalizeCategory(cat) {
	case "otp", "important":
		return 5
	case "bill":
		return 4
	case "marketing":
		return 2
	default:
		return 3
	}
}
