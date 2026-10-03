package email

import (
	"os"
	"strings"
)

// brandMark is the hosted GoMarket lockup used in the header of every email
// this service sends.
//
// Email cannot render inline SVG, and a CID attachment breaks in most webmail,
// so it is a plain PNG served from the marketing site. It is flattened onto
// white rather than left transparent because several Outlook versions
// composite alpha against black, which would swallow the dark wordmark.
func brandMark() string {
	base := os.Getenv("PUBLIC_WEB_URL")
	if strings.TrimSpace(base) == "" {
		base = "https://gomarketi.com"
	}
	return strings.TrimRight(base, "/") + "/email-logo.png"
}
