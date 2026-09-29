terraform {
  required_version = ">= 1.15"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # Bucket/key/region/dynamodb_table are supplied at `terraform init` time via
  # -backend-config=environments/<env>.backend.hcl, so the same root module
  # can target either environment's isolated state file.
  backend "s3" {}
}
