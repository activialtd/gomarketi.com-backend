# Single SG for the single instance — Caddy on the box terminates TLS, so
# only 80/443 are open to the internet. No port 22: shell access is via SSM
# Session Manager (see iam.tf), not SSH.
resource "aws_security_group" "instance" {
  name_prefix = "${local.name_prefix}-instance-"
  description = "GoMarketi ${var.environment} instance - HTTP/HTTPS in, no SSH (SSM only)."
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "HTTPS from internet"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTP from internet (Caddy redirects to HTTPS once a domain is configured)"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "All egress - Neon Postgres, ECR, SSM endpoints, third-party APIs"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "${local.name_prefix}-instance-sg"
  }
}
