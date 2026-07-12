# Default VPC — already has an Internet Gateway and public subnets in every
# region, so the EC2 instance gets a public IP directly. No new VPC/NAT
# needed (NAT Gateway was the single biggest line item in the earlier
# ECS/Fargate design and isn't required for a single public instance).
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }

  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}
