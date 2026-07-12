data "aws_ami" "amazon_linux" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

resource "aws_instance" "main" {
  ami                    = data.aws_ami.amazon_linux.id
  instance_type          = var.instance_type
  subnet_id              = data.aws_subnets.default.ids[0]
  vpc_security_group_ids = [aws_security_group.instance.id]
  iam_instance_profile   = aws_iam_instance_profile.instance.name

  # user_data only runs on first boot — force a clean instance replacement
  # whenever it changes, rather than silently updating the stored attribute
  # without re-running it.
  user_data_replace_on_change = true

  root_block_device {
    volume_size = var.root_volume_size_gb
    volume_type = "gp3"
    encrypted   = true
  }

  user_data = templatefile("${path.module}/user_data.sh.tftpl", {
    environment       = var.environment
    region            = var.region
    ecr_registry      = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${var.region}.amazonaws.com"
    docker_compose    = file("${path.module}/../docker/docker-compose.prod.yml")
    caddyfile         = file("${path.module}/../docker/Caddyfile")
    fetch_env_sh      = file("${path.module}/../docker/fetch-env.sh")
    deploy_service_sh = file("${path.module}/../docker/deploy-service.sh")
  })

  metadata_options {
    http_tokens = "required" # IMDSv2 only
  }

  tags = {
    Name = "${local.name_prefix}-instance"
  }
}

data "aws_caller_identity" "current" {}

# Stable public address to point DNS at — survives instance stop/start
# (unlike the ephemeral public IP EC2 assigns by default). Free while
# attached to a running instance.
resource "aws_eip" "main" {
  domain   = "vpc"
  instance = aws_instance.main.id

  tags = {
    Name = "${local.name_prefix}-eip"
  }
}
