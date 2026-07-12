variable "environment" {
  description = "Deployment environment name (staging or production)."
  type        = string

  validation {
    condition     = contains(["staging", "production"], var.environment)
    error_message = "environment must be \"staging\" or \"production\"."
  }
}

variable "region" {
  description = "AWS region for all resources."
  type        = string
  default     = "eu-west-1"
}

variable "aws_profile" {
  description = "Local AWS CLI profile Terraform authenticates with."
  type        = string
  default     = "gomarketi-terraform"
}

variable "instance_type" {
  description = "EC2 instance type — t3.micro is free-tier eligible in most regions when the account qualifies; otherwise ~$7.60/mo on-demand in eu-west-1."
  type        = string
  default     = "t3.micro"
}

variable "root_volume_size_gb" {
  description = "Root EBS volume size in GB — kept under the 30GB free-tier allowance."
  type        = number
  default     = 20
}

variable "services" {
  description = "Backend services to deploy, keyed by name. Ports match each service's PORT default; container_path is the relative directory under services/ containing its Dockerfile."
  type = map(object({
    port           = number
    container_path = string
  }))
  default = {
    auth = {
      port           = 8080
      container_path = "auth"
    }
    identity = {
      port           = 8081
      container_path = "identity"
    }
    storefront = {
      port           = 8082
      container_path = "storefront"
    }
    catalogue = {
      port           = 8083
      container_path = "catalogue"
    }
    orders = {
      port           = 8084
      container_path = "orders"
    }
    gateway = {
      port           = 8080
      container_path = "gateway"
    }
  }
}

variable "additional_allowed_origins" {
  description = "Extra CORS origins beyond https://gomarketi.com and https://www.gomarketi.com — e.g. frontend local dev / Vercel preview URLs for staging."
  type        = list(string)
  default     = []
}

variable "image_tag" {
  description = "Docker image tag the instance's docker-compose stack pulls. CI updates the running containers via SSM Run Command after pushing a new tag; this var is mainly a reference default for the initial deploy."
  type        = string
  default     = "initial"
}
