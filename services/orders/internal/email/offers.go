package email

import (
	"context"
	"fmt"
	htmlpkg "html"
)

// ── Price offers ──────────────────────────────────────────────────────────────

// OfferMail is what every negotiation email needs to say what happened.
type OfferMail struct {
	OfferID       string
	BuyerToken    string
	StoreSlug     string
	StoreName     string
	ProductName   string
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	ListPriceKobo int64
	AmountKobo    int64 // the figure this email is about
	Message       string
}

// buildOfferURL is the buyer's page for one negotiation on the vendor's
// storefront. The token is what lets a guest in.
func buildOfferURL(storeSlug, offerID, token string) string {
	path := fmt.Sprintf("/offers/%s", offerID)
	if token != "" {
		path += "?token=" + urlEscape(token)
	}
	if rootDomain := getenv("STOREFRONT_ROOT_DOMAIN", ""); rootDomain != "" {
		return fmt.Sprintf("https://%s.%s%s", storeSlug, rootDomain, path)
	}
	localBase := getenv("STOREFRONT_LOCAL_BASE", "localhost:3001")
	return fmt.Sprintf("http://%s.%s%s", storeSlug, localBase, path)
}

func vendorOffersURL() string {
	return getenv("VENDOR_BASE_URL", "http://localhost:3000") + "/merchant/orders/offers"
}

// Kinds of vendor email, one per buyer move worth interrupting a vendor for.
const (
	VendorOfferNew      = "new"      // a buyer offer waiting on the vendor
	VendorOfferInstant  = "instant"  // an offer that cleared the floor and is already a deal
	VendorOfferAccepted = "accepted" // the buyer took the vendor's counter
)

// SendOfferToVendor tells the vendor what a buyer just did in a negotiation.
// Only the new and instant kinds carry the buyer's note, which is only ever
// written with an offer.
func SendOfferToVendor(ctx context.Context, vendorEmail string, m OfferMail, kind string) error {
	url := vendorOffersURL()
	buyer := htmlpkg.EscapeString(m.CustomerName)
	name := htmlpkg.EscapeString(m.ProductName)
	off := percentOff(m.AmountKobo, m.ListPriceKobo)

	var subject, head, lead, label string
	switch kind {
	case VendorOfferInstant:
		subject = fmt.Sprintf("Deal agreed: %s for %s", fmtNaira(m.AmountKobo), m.ProductName)
		head = "An offer cleared your lowest price"
		lead = fmt.Sprintf("%s offered %s for <strong>%s</strong> (%d%% off %s). It is at or above the lowest price you set, so it was accepted for you. They have 48 hours to check out.",
			buyer, fmtNaira(m.AmountKobo), name, off, fmtNaira(m.ListPriceKobo))
		label = "See the deal"
	case VendorOfferAccepted:
		subject = fmt.Sprintf("Deal agreed: %s took your counter on %s", m.CustomerName, m.ProductName)
		head = "Your counter-offer was accepted"
		lead = fmt.Sprintf("%s agreed to %s for <strong>%s</strong>. They have 48 hours to check out.",
			buyer, fmtNaira(m.AmountKobo), name)
		label = "See the deal"
	default:
		subject = fmt.Sprintf("New offer: %s for %s", fmtNaira(m.AmountKobo), m.ProductName)
		head = "You have a new offer"
		lead = fmt.Sprintf("%s offered %s for <strong>%s</strong> (%d%% off %s). Accept it, counter, or decline — it lapses in 48 hours if nobody replies.",
			buyer, fmtNaira(m.AmountKobo), name, off, fmtNaira(m.ListPriceKobo))
		label = "Reply to the offer"
	}

	body := heading(head) + paragraph(lead)
	if m.Message != "" {
		body += labelRow("Their note") + paragraph("“"+htmlpkg.EscapeString(m.Message)+"”")
	}
	body += labelRow("Buyer") + paragraph(contactLine(m.CustomerName, m.CustomerEmail, m.CustomerPhone))
	body += button(label, url)

	plain := fmt.Sprintf("%s\n%s", subject, url)
	return sendMail(ctx, vendorEmail, subject, shell(head, subject, body), plain)
}

// SendOfferUpdateToBuyer tells the buyer the vendor has moved: accepted,
// countered, or declined. Expiry is quiet — the buyer's offer page says so.
func SendOfferUpdateToBuyer(ctx context.Context, action string, m OfferMail) error {
	url := buildOfferURL(m.StoreSlug, m.OfferID, m.BuyerToken)
	name := htmlpkg.EscapeString(m.ProductName)
	store := htmlpkg.EscapeString(m.StoreName)

	var subject, head, lead, label string
	switch action {
	case "accept":
		subject = fmt.Sprintf("%s accepted your offer of %s", m.StoreName, fmtNaira(m.AmountKobo))
		head = "Your offer was accepted"
		lead = fmt.Sprintf("%s agreed to sell you <strong>%s</strong> for %s. The price is held for you for 48 hours.", store, name, fmtNaira(m.AmountKobo))
		label = fmt.Sprintf("Buy at %s", fmtNaira(m.AmountKobo))
	case "counter":
		subject = fmt.Sprintf("%s countered at %s", m.StoreName, fmtNaira(m.AmountKobo))
		head = "The seller made a counter-offer"
		lead = fmt.Sprintf("%s can do <strong>%s</strong> for %s. You can accept it, decline it, or come back with another offer.", store, name, fmtNaira(m.AmountKobo))
		label = "See the counter-offer"
	case "decline":
		subject = fmt.Sprintf("%s declined your offer for %s", m.StoreName, m.ProductName)
		head = "Your offer was declined"
		lead = fmt.Sprintf("%s couldn't take %s for <strong>%s</strong>. It is still available at %s.", store, fmtNaira(m.AmountKobo), name, fmtNaira(m.ListPriceKobo))
		label = "View the offer"
	default:
		return nil
	}

	body := heading(head) + paragraph(lead)
	if m.Message != "" {
		body += labelRow("Note from the seller") + paragraph("“"+htmlpkg.EscapeString(m.Message)+"”")
	}
	body += button(label, url)

	plain := fmt.Sprintf("%s\n%s", subject, url)
	return sendMail(ctx, m.CustomerEmail, subject, shell(head, subject, body), plain)
}

func contactLine(name, email, phone string) string {
	line := htmlpkg.EscapeString(name) + " · " + htmlpkg.EscapeString(email)
	if phone != "" {
		line += " · " + htmlpkg.EscapeString(phone)
	}
	return line
}

func percentOff(amount, list int64) int64 {
	if list <= 0 || amount >= list {
		return 0
	}
	return (list - amount) * 100 / list
}
