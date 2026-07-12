# Resources shared across ALL environments (not per-staging/production) —
# right now, just the ECR repositories. Images are built once and the same
# tag is promoted from staging to production, so repos are account-global,
# not environment-scoped. Applied independently from the staging/production
# root module (see ../environments/shared.backend.hcl).
#
# Usage:
#   cd infrastructure/terraform/shared
#   terraform init -backend-config=../environments/shared-state.backend.hcl
#   terraform plan  -var-file=../environments/shared.tfvars
#   terraform apply -var-file=../environments/shared.tfvars

terraform {
  required_version = ">= 1.15"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {}
}

provider "aws" {
  region  = var.region
  profile = var.aws_profile

  default_tags {
    tags = {
      Project   = "gomarketi"
      ManagedBy = "terraform"
      Scope     = "shared"
    }
  }
}

variable "region" {
  type    = string
  default = "eu-west-1"
}

variable "aws_profile" {
  type    = string
  default = "gomarketi-terraform"
}

variable "services" {
  type    = set(string)
  default = ["auth", "identity", "storefront", "catalogue", "orders", "gateway"]
}

resource "aws_ecr_repository" "services" {
  for_each = var.services

  name                 = "gomarketi-${each.key}"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = {
    Name    = "gomarketi-${each.key}"
    Service = each.key
  }
}

resource "aws_ecr_lifecycle_policy" "services" {
  for_each   = aws_ecr_repository.services
  repository = each.value.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images after 14 days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 14
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only the last 20 images"
        selection = {
          tagStatus   = "any"
          countType   = "imageCountMoreThan"
          countNumber = 20
        }
        action = { type = "expire" }
      }
    ]
  })
}

output "repository_urls" {
  value = { for k, r in aws_ecr_repository.services : k => r.repository_url }
}

output "repository_arns" {
  value = { for k, r in aws_ecr_repository.services : k => r.arn }
}

# ── GitHub OIDC: lets CI assume an AWS role without any long-lived keys ──────

variable "github_repo" {
  description = "GitHub repo allowed to assume the deploy role, as org/repo."
  type        = string
  default     = "activialtd/gomarketi.com-backend"
}

resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1", "1c58a3a8518e8759bf075b76b750d4f2df264fcd"]
}

data "aws_iam_policy_document" "github_actions_assume" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }
    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:${var.github_repo}:*"]
    }
  }
}

resource "aws_iam_role" "github_actions_deploy" {
  name               = "gomarketi-github-actions-deploy"
  assume_role_policy = data.aws_iam_policy_document.github_actions_assume.json
}

# Both staging and production instances are targetable by this one role —
# the workflow picks the instance ID at runtime based on which branch/
# environment triggered it.
data "aws_instance" "staging" {
  filter {
    name   = "tag:Name"
    values = ["gomarketi-staging-instance"]
  }
  filter {
    name   = "instance-state-name"
    values = ["running"]
  }
}

data "aws_instance" "production" {
  filter {
    name   = "tag:Name"
    values = ["gomarketi-production-instance"]
  }
  filter {
    name   = "instance-state-name"
    values = ["running"]
  }
}

data "aws_iam_policy_document" "github_actions_deploy" {
  statement {
    sid       = "EcrAuth"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    sid = "EcrPush"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:PutImage",
      "ecr:InitiateLayerUpload",
      "ecr:UploadLayerPart",
      "ecr:CompleteLayerUpload",
      "ecr:BatchGetImage",
      "ecr:GetDownloadUrlForLayer",
    ]
    resources = [for r in aws_ecr_repository.services : r.arn]
  }

  statement {
    sid       = "ResolveInstanceId"
    actions   = ["ec2:DescribeInstances"]
    resources = ["*"]
  }

  statement {
    sid = "DeployViaSsm"
    actions = [
      "ssm:SendCommand",
    ]
    resources = [
      "arn:aws:ssm:*:*:document/AWS-RunShellScript",
      data.aws_instance.staging.arn,
      data.aws_instance.production.arn,
    ]
  }

  statement {
    sid = "ReadCommandResults"
    actions = [
      "ssm:GetCommandInvocation",
      "ssm:ListCommandInvocations",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "github_actions_deploy" {
  name   = "ecr-push-and-ssm-deploy"
  role   = aws_iam_role.github_actions_deploy.id
  policy = data.aws_iam_policy_document.github_actions_deploy.json
}

output "github_actions_role_arn" {
  value = aws_iam_role.github_actions_deploy.arn
}

output "staging_instance_id" {
  value = data.aws_instance.staging.id
}

output "production_instance_id" {
  value = data.aws_instance.production.id
}
