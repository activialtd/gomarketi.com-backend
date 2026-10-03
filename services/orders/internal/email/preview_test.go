package email

import (
	"os"
	"testing"
)

// TestRenderPreviews writes each template to /tmp/mailpreview so the result
// can be looked at in a browser instead of imagined. It asserts the things
// that are easy to get wrong in email HTML and invisible in review: that the
// brand image is present, that no template still leads with a decorative
// emoji, and that the preheader is set so inboxes do not show alt text.
func TestRenderPreviews(t *testing.T) {
	dir := "/tmp/mailpreview"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	items := []InvoiceItem{
		{Name: "Iphone 14 Pro 128GB", Quantity: 1, PriceKobo: 28500000},
		{Name: "Anker 20W USB-C Charger", Quantity: 2, PriceKobo: 1450000},
	}

	cases := map[string]string{
		"status-shipped.html":   statusUpdateHTML("Ada Obi", "Xemp Tech", "ee40078c-d619-4e73-8942-d6bcf65ae603", "https://xemp-tech.gomarketi.com/orders/ee40078c?email=a%40b.com", "shipped"),
		"status-confirmed.html": statusUpdateHTML("Ada Obi", "Xemp Tech", "ee40078c-d619-4e73-8942-d6bcf65ae603", "https://example.com", "confirmed"),
		"status-cancelled.html": statusUpdateHTML("Ada Obi", "Xemp Tech", "ee40078c-d619-4e73-8942-d6bcf65ae603", "https://example.com", "cancelled"),
		"invoice.html":          customerInvoiceHTML("Ada Obi", "Xemp Tech", "ee40078c-d619-4e73-8942-d6bcf65ae603", "https://example.com", 31410000, 450000, "Ikeja Under Bridge", items),
		"vendor-alert.html":     vendorAlertHTML("Xemp Tech", "ee40078c-d619-4e73-8942-d6bcf65ae603", "https://vendor.gomarketi.com", "Ada Obi", "ada@example.com", "08031234567", "12 Allen Avenue, Ikeja, Lagos", 31410000, 450000, "Ikeja Under Bridge", items),
		"cart-summary.html":     cartSummaryHTML("Ada Obi", "Xemp Tech", "https://example.com", 31410000, items),
	}

	for name, body := range cases {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			if !contains(body, "email-logo.png") {
				t.Error("no brand mark — every message should carry the logo")
			}
			for _, e := range []string{"🎉", "🚀", "✨", "🇳🇬", "😊"} {
				if contains(body, e) {
					t.Errorf("decorative emoji %q still present", e)
				}
			}
			if !contains(body, "max-height:0") {
				t.Error("no preheader block — inboxes will show the logo's alt text instead")
			}
		})
	}
	t.Logf("wrote %d previews to %s", len(cases), dir)
}

func contains(h, needle string) bool {
	return len(h) > 0 && len(needle) > 0 && (func() bool {
		for i := 0; i+len(needle) <= len(h); i++ {
			if h[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
