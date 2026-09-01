environment = "staging"
region      = "eu-west-1"
aws_profile = "gomarketi-terraform"

# Frontend dev/testing origins.
# vendor.gomarketi.com is normally the PRODUCTION vendor dashboard domain —
# it's temporarily pointed at this staging API on purpose (2026-07-29,
# deliberate choice, not a mistake) while validating the AWS backend before
# the real production cutover. Revert once that validation is done.
additional_allowed_origins = [
  "http://localhost:3000",
  "https://vendor.gomarketi.com",
  "https://gomarketi-staging.vercel.app", # placeholder — replace with real staging-vendor URL once live
  # Every vendor storefront lives on its own subdomain (e.g.
  # timeride.gomarketi.com) — without this wildcard, checkout (order
  # creation, error reporting) is CORS-blocked from every single vendor's
  # storefront except whatever happens to be hardcoded above. See
  # gateway's originMatches() for the "*.host" wildcard support this relies on.
  "https://*.gomarketi.com",
  # admin-web (Admin Center) doesn't have a custom domain yet — it's only
  # reachable at whatever Vercel-assigned URL its project currently has.
  # This will need updating (or replacing with a real admin.gomarketi.com
  # entry, matched by the wildcard above) whenever that project's URL
  # changes, since Vercel's *.vercel.app aliases aren't guaranteed stable
  # across every redeploy.
  "https://gomarketi-com-frontend-vendor-web-p.vercel.app",
]
