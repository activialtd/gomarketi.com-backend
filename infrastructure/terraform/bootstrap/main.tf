# One-time bootstrap: creates the S3 bucket + DynamoDB table that the main
# infrastructure/terraform root module uses as its remote state backend.
#
# This config intentionally uses LOCAL state (chicken-and-egg: the backend
# storage can't store its own creation state). Apply once, then never again
# unless the backend itself needs to change.
#
# Usage:
#   cd infrastructure/terraform/bootstrap
#   terraform init
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
}

provider "aws" {
  region  = var.region
  profile = var.aws_profile
}

variable "region" {
  description = "AWS region for the state backend."
  type        = string
  default     = "eu-west-1"
}

variable "aws_profile" {
  description = "Local AWS CLI profile Terraform should authenticate with."
  type        = string
  default     = "gomarketi-terraform"
}

variable "state_bucket_name" {
  description = "Globally-unique S3 bucket name for Terraform remote state."
  type        = string
}

variable "lock_table_name" {
  description = "DynamoDB table name for Terraform state locking."
  type        = string
  default     = "gomarketi-terraform-lock"
}

resource "aws_s3_bucket" "terraform_state" {
  bucket = var.state_bucket_name

  # Safety net: `terraform destroy` on this bootstrap config won't silently
  # nuke all prior infra state history.
  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_s3_bucket_versioning" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "aws:kms"
    }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_public_access_block" "terraform_state" {
  bucket                  = aws_s3_bucket.terraform_state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_dynamodb_table" "terraform_lock" {
  name         = var.lock_table_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "LockID"

  attribute {
    name = "LockID"
    type = "S"
  }
}

output "state_bucket_name" {
  value = aws_s3_bucket.terraform_state.bucket
}

output "lock_table_name" {
  value = aws_dynamodb_table.terraform_lock.name
}
