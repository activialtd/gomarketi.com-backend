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
]
