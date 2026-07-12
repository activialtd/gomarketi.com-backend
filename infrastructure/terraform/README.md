# GoMarketi AWS infrastructure

Terraform for the single-EC2-instance migration off Railway (cost-minimized
design — no ECS/Fargate/ALB/ElastiCache). See the migration plan for full
context and rollout order.

## Architecture

One EC2 instance per environment (`t3.micro`, default VPC, public subnet,
Elastic IP), running all 6 Go services + `redis:7-alpine` + `caddy` via
Docker Compose (`infrastructure/docker/docker-compose.prod.yml`). Caddy
terminates TLS (automatic Let's Encrypt once a domain is configured) and
reverse-proxies to `gateway`. Redis never leaves the instance's internal
Docker network. No SSH — shell access via SSM Session Manager, deploys via
SSM Run Command.

## One-time setup (already done)

- IAM user `terraform-gomarketi`, policy `gomarketi-terraform-policy` (EC2 +
  ECR + SSM + scoped IAM permissions), credentials in AWS CLI profile
  `gomarketi-terraform`.
- State backend: S3 bucket `gomarketi-terraform-state-336617737576` +
  DynamoDB table `gomarketi-terraform-lock` (via `bootstrap/`).

## Usage

```bash
cd infrastructure/terraform
terraform init -backend-config=environments/staging.backend.hcl
terraform plan  -var-file=environments/staging.tfvars
terraform apply -var-file=environments/staging.tfvars
```

Swap `staging` for `production` to target the other environment (separate
state file, same backend, separate EC2 instance). If you've already `init`'d
one environment and need to switch, run
`terraform init -reconfigure -backend-config=...` for the other.

## Populating secrets

`ssm_parameters.tf` creates one SecureString SSM parameter per
(service, key) pair at `/gomarketi/<env>/<service>/<KEY>`, seeded with a
placeholder. After `terraform apply`, populate real values manually —
**never** put real secrets in `.tf`/`.tfvars` files or commit them:

```bash
aws ssm put-parameter \
  --profile gomarketi-terraform --region eu-west-1 \
  --name /gomarketi/staging/auth/DATABASE_URL \
  --type SecureString --overwrite \
  --value "postgresql://..."
```

Values come from the untracked `.env.railway` file at the repo root (real
prod values today) or `.env.railway.template` (tracked, key names only).
Expected keys per service (must match `local.service_secret_keys` in
`locals.tf` exactly):

| Service | Keys |
|---|---|
| auth | `DATABASE_URL`, `JWT_PRIVATE_KEY_B64`, `JWT_PUBLIC_KEY_B64`, `BREVO_API_KEY`, `RESEND_API_KEY`, `GMAIL_CLIENT_ID`, `GMAIL_CLIENT_SECRET`, `GMAIL_REFRESH_TOKEN`, `MAILGUN_API_KEY`, `MAILGUN_DOMAIN`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, `GOOGLE_CLIENT_ID`, `APPLE_BUNDLE_ID` |
| identity | `DATABASE_URL`, `ENCRYPTION_KEY`, `SMILE_ID_PARTNER_ID`, `SMILE_ID_API_KEY` |
| storefront | `DATABASE_URL`, `BREVO_API_KEY`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, `SUPABASE_S3_ENDPOINT`, `SUPABASE_S3_REGION`, `SUPABASE_S3_BUCKET`, `SUPABASE_PUBLIC_URL`, `SUPABASE_S3_ACCESS_KEY_ID`, `SUPABASE_S3_SECRET_ACCESS_KEY` |
| catalogue | `DATABASE_URL` |
| orders | `DATABASE_URL`, `REDIS_URL`, `PAYSTACK_SECRET_KEY`, `GMAIL_CLIENT_ID`, `GMAIL_CLIENT_SECRET`, `GMAIL_REFRESH_TOKEN`, `GMAIL_FROM`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, `STOREFRONT_ROOT_DOMAIN`, `STOREFRONT_LOCAL_BASE`, `VENDOR_BASE_URL` |
| gateway | `JWT_PUBLIC_KEY` |

`orders`' `REDIS_URL` is simply `redis://redis:6379/0` — Redis runs on the
same instance's internal Docker network only, so no TLS/AUTH is needed.

## Initial images + first deploy

Build and push once per service before the instance's first boot finishes
(or the containers will fail to pull `:initial` and retry until you do):

```bash
aws ecr get-login-password --profile gomarketi-terraform --region eu-west-1 \
  | docker login --username AWS --password-stdin <account-id>.dkr.ecr.eu-west-1.amazonaws.com

for s in auth identity storefront catalogue orders gateway; do
  docker build -f services/$s/Dockerfile -t gomarketi-$s:initial .
  docker tag gomarketi-$s:initial <account-id>.dkr.ecr.eu-west-1.amazonaws.com/gomarketi-$s:initial
  docker push <account-id>.dkr.ecr.eu-west-1.amazonaws.com/gomarketi-$s:initial
done
```

Then, after populating the SSM parameters, trigger a redeploy on the
instance via SSM (or just wait for the next boot's systemd unit):

```bash
aws ssm send-command \
  --profile gomarketi-terraform --region eu-west-1 \
  --instance-ids <instance-id-from-terraform-output> \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["/opt/gomarketi/deploy.sh"]'
```

## Shell access (no SSH)

```bash
aws ssm start-session --profile gomarketi-terraform --region eu-west-1 \
  --target <instance-id>
```

## Cutover

`SITE_ADDRESS` in `/opt/gomarketi/.env` defaults to `:80` (plain HTTP,
for smoke-testing via the Elastic IP before DNS is involved). Once DNS
points a domain at the Elastic IP, SSM in, edit `/opt/gomarketi/.env` to
set `SITE_ADDRESS=api.gomarketi.com` (or whichever domain), then
`docker compose -f docker-compose.prod.yml restart caddy` — Caddy will
automatically request and renew a Let's Encrypt certificate for that
domain.
