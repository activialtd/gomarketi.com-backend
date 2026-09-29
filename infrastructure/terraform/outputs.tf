output "instance_public_ip" {
  description = "Elastic IP — smoke-test via curl before DNS cutover, and point DNS here at cutover time."
  value       = aws_eip.main.public_ip
}

output "instance_id" {
  description = "EC2 instance ID — target this with `aws ssm start-session` / `aws ssm send-command`."
  value       = aws_instance.main.id
}

output "ecr_repository_urls" {
  description = "ECR repository URLs, one per service."
  value       = local.ecr_repo_urls
}

output "ssm_parameter_names" {
  description = "SSM parameter names created per (service, key) — populate real values here."
  value       = { for k, p in aws_ssm_parameter.service_secrets : k => p.name }
}
