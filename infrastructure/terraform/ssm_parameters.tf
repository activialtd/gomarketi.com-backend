# One SecureString SSM parameter per (service, key) pair — permanently free
# (standard-tier parameters), encrypted with the default aws/ssm KMS key.
# Terraform only creates the parameter with a placeholder value; real
# values are populated manually afterward (out-of-band, sourced from the
# untracked .env.railway file), never written into .tf/.tfvars.
resource "aws_ssm_parameter" "service_secrets" {
  for_each = local.ssm_param_pairs

  name  = "/gomarketi/${var.environment}/${each.value.service}/${each.value.key}"
  type  = "SecureString"
  value = "populate-me"

  tags = {
    Service = each.value.service
  }

  lifecycle {
    # Once real values are written out-of-band, don't let `terraform apply`
    # stomp them back to the placeholder.
    ignore_changes = [value]
  }
}
