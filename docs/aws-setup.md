# AWS + DNS Setup Guide

Two environments:
- **Staging** → `api.staging.gomarketi.com`
- **Production** → `api.gomarketi.com`

---

## Step 1 — AWS IAM: GitHub Actions OIDC Role

This lets GitHub Actions deploy to AWS without storing any AWS keys.

1. Go to **IAM → Identity Providers → Add provider**
   - Provider type: `OpenID Connect`
   - Provider URL: `https://token.actions.githubusercontent.com`
   - Audience: `sts.amazonaws.com`

2. Go to **IAM → Roles → Create role**
   - Trusted entity: `Web identity`
   - Identity provider: `token.actions.githubusercontent.com`
   - Audience: `sts.amazonaws.com`
   - Add condition: `token.actions.githubusercontent.com:sub` = `repo:YOUR_GITHUB_ORG/gomarketi.com-backend:*`
   - Attach policies: `AmazonECS_FullAccess`, `AmazonEC2ContainerRegistryFullAccess`
   - Name the role: `GoMarketi-GitHubActions-Role`

3. Copy the **Role ARN** — you'll need it for GitHub secrets.

---

## Step 2 — Amazon ECR (Container Registry)

One repository per microservice. Repeat for each service:

1. Go to **ECR → Create repository**
   - Visibility: `Private`
   - Name: `user-service` (repeat for each service)
2. Note your **ECR Registry URL**: `ACCOUNT_ID.dkr.ecr.REGION.amazonaws.com`

---

## Step 3 — ECS Fargate Clusters

Create **two clusters** (staging and production):

1. Go to **ECS → Clusters → Create cluster**
   - Name: `gomarketi-staging`
   - Infrastructure: `AWS Fargate (serverless)` ✅
2. Repeat, name the second: `gomarketi-production`

---

## Step 4 — Application Load Balancer (ALB)

Create **two ALBs** (one per environment):

1. Go to **EC2 → Load Balancers → Create → Application Load Balancer**
   - Name: `gomarketi-staging-alb`
   - Scheme: `Internet-facing`
   - Listeners: `HTTP:80` and `HTTPS:443`
   - VPC: use your default VPC, select all AZs
2. Repeat for `gomarketi-production-alb`

For each ALB, add a listener rule:
- HTTP:80 → **Redirect to HTTPS:443**
- HTTPS:443 → forward to target groups (one per service — add as you build)

---

## Step 5 — SSL Certificate (ACM)

1. Go to **ACM → Request certificate → Public certificate**
2. Add domain names:
   - `api.gomarketi.com`
   - `api.staging.gomarketi.com`
3. Validation method: `DNS validation`
4. ACM will give you **CNAME records** to add in Hostinger (Step 6A)
5. Once validated, attach the certificate to both ALB HTTPS listeners

---

## Step 6A — Hostinger DNS: ACM Validation CNAMEs

In Hostinger DNS for `gomarketi.com`, add the CNAME records ACM gives you.
They look like:
```
_abc123.api        CNAME   _xyz789.acm-validations.aws.
_abc123.api.staging  CNAME   _xyz789.acm-validations.aws.
```
Wait ~10 minutes for validation to complete.

---

## Step 6B — Hostinger DNS: Point to ALBs

Once ACM certs are issued, add these records in Hostinger:

| Type  | Name              | Value                                      |
|-------|-------------------|--------------------------------------------|
| CNAME | `api`             | `gomarketi-production-alb-xxxx.us-east-1.elb.amazonaws.com` |
| CNAME | `api.staging`     | `gomarketi-staging-alb-xxxx.us-east-1.elb.amazonaws.com`    |

Get the ALB DNS names from **EC2 → Load Balancers**.

---

## Step 7 — ECS Task Definitions + Services

For each microservice (repeat as you build them):

1. **Task Definition** → Create new
   - Launch type: `Fargate`
   - Name: `user-service`
   - CPU: `256` (.25 vCPU), Memory: `512 MB` (increase as needed)
   - Container:
     - Name: `user-service`
     - Image: `ECR_REGISTRY/user-service:latest`
     - Port: `3001`
     - Environment variables: add your `.env` vars here

2. **ECS Service** → Create in your cluster
   - Cluster: `gomarketi-staging` or `gomarketi-production`
   - Service name: `user-service`
   - Task definition: `user-service`
   - Desired tasks: `1` (staging) / `2+` (production)
   - Load balancer: attach to your ALB, create a new target group
   - Add ALB listener rule: path `/users/*` → this target group

---

## Step 8 — GitHub Repository Variables & Secrets

Go to **GitHub → Repo Settings → Environments** and set these for each environment:

### Both environments (staging + production)
| Name | Value |
|------|-------|
| `AWS_ROLE_ARN` | `arn:aws:iam::ACCOUNT_ID:role/GoMarketi-GitHubActions-Role` |
| `AWS_REGION` | `us-east-1` (or your region) |
| `ECR_REGISTRY` | `ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com` |
| `ECS_CLUSTER` | `gomarketi-staging` or `gomarketi-production` |

---

## Step 9 — Local Dev: ngrok

1. Sign up at [ngrok.com](https://ngrok.com) (free tier works)
2. Copy your auth token from the dashboard
3. Add it to your `.env`: `NGROK_AUTHTOKEN=your_token`
4. Update `ngrok.yml` — uncomment the services you're running locally
5. Run: `ngrok start --all --config=ngrok.yml`

Each service gets a public URL like `https://abc123.ngrok-free.app` for testing webhooks, sharing with teammates, etc.

---

## Flow Summary

```
Local dev  →  ngrok tunnel  →  public URL for testing
Push to staging branch  →  GitHub Actions  →  ECR  →  ECS staging  →  api.staging.gomarketi.com
Push to main branch     →  GitHub Actions  →  ECR  →  ECS prod     →  api.gomarketi.com
```
