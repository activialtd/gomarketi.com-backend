environment = "staging"
region      = "eu-west-1"
aws_profile = "gomarketi-terraform"

# Frontend dev/testing origins — update the Vercel URL once the frontend
# team gives it to you (their staging deployment's actual domain).
additional_allowed_origins = [
  "http://localhost:3000",
  "https://gomarketi-staging.vercel.app", # placeholder — replace with real URL
]
