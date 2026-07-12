# ECR repositories are account-global, not per-environment (build once,
# promote the same image tag from staging to production) — they're managed
# in infrastructure/terraform/shared/, not here. Read-only reference.
data "aws_ecr_repository" "services" {
  for_each = var.services
  name     = "gomarketi-${each.key}"
}
