package service

import "testing"

// Paystack refuses dedicated_account creation without a customer phone, and
// the two sources we draw from are stored in different shapes — storefront
// writes "234XXXXXXXXXX", signup may write a local "0XXXXXXXXXX". Both have
// to come out as E.164 or the call fails with a message the vendor never sees.
func TestNormalisePhone(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"storefront support phone", "2348031234567", "+2348031234567"},
		{"already e164", "+2348031234567", "+2348031234567"},
		{"nigerian local", "08031234567", "+2348031234567"},
		{"bare subscriber", "8031234567", "+2348031234567"},
		{"spaces and dashes", "0803 123-4567", "+2348031234567"},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"too short", "12345", ""},
		{"not a number", "n/a", ""},
		{"too long", "23480312345678901", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalisePhone(tc.in); got != tc.want {
				t.Errorf("normalisePhone(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
