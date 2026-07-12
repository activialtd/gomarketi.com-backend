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
