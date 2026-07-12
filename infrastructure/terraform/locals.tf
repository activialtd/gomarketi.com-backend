locals {
  name_prefix = "gomarketi-${var.environment}"

  allowed_origins = join(",", concat(
    ["https://gomarketi.com", "https://www.gomarketi.com"],
    var.additional_allowed_origins,
  ))

  ecr_repo_urls = { for k, repo in data.aws_ecr_repository.services : k => repo.repository_url }

  # SSM parameter keys expected per service (gomarketi/<env>/<service>/<KEY>)
  # — operators populate the actual values manually (see README.md).
  service_secret_keys = {
    auth = [
      "DATABASE_URL", "JWT_PRIVATE_KEY_B64", "JWT_PUBLIC_KEY_B64",
      "BREVO_API_KEY", "RESEND_API_KEY", "GMAIL_CLIENT_ID", "GMAIL_CLIENT_SECRET",
      "GMAIL_REFRESH_TOKEN", "MAILGUN_API_KEY", "MAILGUN_DOMAIN",
      "SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM",
      "GOOGLE_CLIENT_ID", "APPLE_BUNDLE_ID",
    ]
    identity = [
      "DATABASE_URL", "ENCRYPTION_KEY", "SMILE_ID_PARTNER_ID", "SMILE_ID_API_KEY",
    ]
    storefront = [
      "DATABASE_URL", "BREVO_API_KEY",
      "SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM",
      "SUPABASE_S3_ENDPOINT", "SUPABASE_S3_REGION", "SUPABASE_S3_BUCKET",
      "SUPABASE_PUBLIC_URL", "SUPABASE_S3_ACCESS_KEY_ID", "SUPABASE_S3_SECRET_ACCESS_KEY",
    ]
    catalogue = [
      "DATABASE_URL",
    ]
    orders = [
      # REDIS_URL here is trivial (redis://redis:6379/0) — Redis never
      # leaves the instance's internal Docker network, so no TLS/AUTH.
      "DATABASE_URL", "REDIS_URL", "PAYSTACK_SECRET_KEY",
      "GMAIL_CLIENT_ID", "GMAIL_CLIENT_SECRET", "GMAIL_REFRESH_TOKEN", "GMAIL_FROM",
      "SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM",
      "STOREFRONT_ROOT_DOMAIN", "STOREFRONT_LOCAL_BASE", "VENDOR_BASE_URL",
    ]
    gateway = [
      "JWT_PUBLIC_KEY",
    ]
  }

  # Plain (non-secret) env vars per service — same values used in
  # docker-compose.prod.yml. UPSTREAM_* use Docker Compose service-name DNS.
  service_plain_env = {
    auth = {
      ENV                     = var.environment
      ALLOWED_ORIGINS         = local.allowed_origins
      JWT_ACCESS_TTL_SECONDS  = "900"
      JWT_REFRESH_TTL_SECONDS = "2592000"
      BREVO_FROM              = "noreply@gomarketi.com"
      BREVO_FROM_NAME         = "GoMarketi"
    }
    identity = {
      ENV              = var.environment
      ALLOWED_ORIGINS  = local.allowed_origins
      SMILE_ID_SANDBOX = var.environment == "production" ? "false" : "true"
    }
    storefront = {
      ENV             = var.environment
      ALLOWED_ORIGINS = local.allowed_origins
      STORE_DOMAIN    = "gomarketi.com"
    }
    catalogue = {
      ENV             = var.environment
      ALLOWED_ORIGINS = local.allowed_origins
    }
    orders = {
      ENV             = var.environment
      ALLOWED_ORIGINS = local.allowed_origins
    }
    gateway = {
      ALLOWED_ORIGINS     = local.allowed_origins
      UPSTREAM_AUTH       = "http://auth:${var.services["auth"].port}"
      UPSTREAM_STOREFRONT = "http://storefront:${var.services["storefront"].port}"
      UPSTREAM_IDENTITY   = "http://identity:${var.services["identity"].port}"
      UPSTREAM_CATALOGUE  = "http://catalogue:${var.services["catalogue"].port}"
      UPSTREAM_ORDERS     = "http://orders:${var.services["orders"].port}"
    }
  }

  # Flattened (service, key) pairs — one SSM SecureString parameter each,
  # driving the for_each in ssm_parameters.tf.
  ssm_param_pairs = merge([
    for svc, keys in local.service_secret_keys : {
      for k in keys : "${svc}/${k}" => { service = svc, key = k }
    }
  ]...)
}
