package email

import (
	"fmt"
	htmlpkg "html"
	"strings"
)

// One shell for every order email.
//
// Each template used to carry its own full HTML document, so the brand drifted
// between them and a change meant editing five copies. They also led with a
// 32px emoji and copy like "Great news — your order is on its way!", which
// reads as filler rather than information. These are transactional receipts:
// the useful parts are the amount, the items and the link, and the design's
// job is to get out of their way.

// brandMark is the hosted lockup. Email cannot use inline SVG, and a CID
// attachment breaks in webmail, so it is a plain PNG on the marketing site.
// Flattened onto white because several Outlook versions composite alpha
// against black and would swallow the dark wordmark.
func brandMark() string {
	base := strings.TrimRight(getenv("PUBLIC_WEB_URL", "https://gomarketi.com"), "/")
	return base + "/email-logo.png"
}

const (
	inkColor     = "#0E1F13"
	brandGreen   = "#046244"
	mutedColor   = "#6b7280"
	hairlineGrey = "#e8ecea"
	pageBg       = "#f4f6f5"
)

// shell wraps body in the shared frame: logo, white card, footer.
//
// preheader is the grey line mail clients show beside the subject in an
// inbox list. Left unset it leaks whatever text comes first, which is usually
// the logo's alt text — so every template sets it deliberately.
func shell(title, preheader, body string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="x-apple-disable-message-reformatting">
<title>%s</title>
</head>
<body style="margin:0;padding:0;background:%s;-webkit-font-smoothing:antialiased;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">

<div style="display:none;max-height:0;overflow:hidden;opacity:0;">%s</div>

<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="background:%s;">
<tr><td align="center" style="padding:32px 16px 40px;">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="max-width:600px;width:100%%;">

  <tr><td align="center" style="padding:0 0 24px;">
    <img src="%s" width="150" alt="GoMarket" style="display:block;border:0;width:150px;height:auto;">
  </td></tr>

  <tr><td style="background:#ffffff;border:1px solid %s;border-radius:14px;padding:36px 36px 32px;">
%s
  </td></tr>

  <tr><td style="padding:22px 8px 0;text-align:center;">
    <p style="margin:0;font-size:12px;line-height:1.7;color:%s;">
      GoMarket · Shop your local market without the trip<br>
      Questions? Reply to this email and a person will answer.
    </p>
  </td></tr>

</table>
</td></tr>
</table>
</body>
</html>`, htmlpkg.EscapeString(title), pageBg, htmlpkg.EscapeString(preheader), pageBg, brandMark(), hairlineGrey, body, mutedColor)
}

// heading is the one large line in a message. Sentence case and no exclamation
// mark: these arrive because something happened, not to celebrate it.
func heading(text string) string {
	return fmt.Sprintf(
		`<h1 style="margin:0 0 10px;font-size:20px;line-height:1.3;font-weight:700;color:%s;letter-spacing:-0.3px;">%s</h1>`,
		inkColor, htmlpkg.EscapeString(text))
}

func paragraph(text string) string {
	return fmt.Sprintf(
		`<p style="margin:0 0 18px;font-size:14.5px;line-height:1.65;color:#374151;">%s</p>`, text)
}

// button is a table rather than a styled anchor so Outlook renders the fill.
func button(label, url string) string {
	return fmt.Sprintf(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:4px 0 6px;">
<tr><td align="center" style="background:%s;border-radius:9px;">
  <a href="%s" style="display:inline-block;padding:13px 26px;font-size:14px;font-weight:700;color:#ffffff;text-decoration:none;letter-spacing:0.1px;">%s</a>
</td></tr></table>`, brandGreen, url, htmlpkg.EscapeString(label))
}

// labelRow is the small caps label used above a block of detail.
func labelRow(text string) string {
	return fmt.Sprintf(
		`<p style="margin:26px 0 10px;font-size:11px;font-weight:700;letter-spacing:0.09em;text-transform:uppercase;color:%s;">%s</p>`,
		mutedColor, htmlpkg.EscapeString(text))
}

func divider() string {
	return fmt.Sprintf(`<div style="height:1px;background:%s;margin:24px 0;"></div>`, hairlineGrey)
}
