# local — Railway deploy from this machine

Creates one Railway project with two services (`api`, `worker`) and deploys them
by uploading the repository from your machine. The api's public domain is not
managed here — generate it once in the Railway UI (Settings → Networking). Railway is never connected to
GitHub: `railway_service` has no `source_repo`, and the code is shipped by
`railway up` from a `local-exec` provisioner.

Migrations are not a separate service — `Dockerfile.api` and `Dockerfile.worker`
already run `/app/migrate` before the main binary.

## Prerequisites

```sh
brew install railway   # the CLI is required, terraform shells out to it
railway --version
terraform -version     # >= 1.6
```

A Railway account **verified** at https://railway.com/verify. Unverified trial
accounts get restricted outbound networking, and the scraper cannot reach OLX.

## Usage

```sh
cp terraform.tfvars.example terraform.tfvars   # fill in railway_token + db_url
terraform init
terraform plan
terraform apply
```

Then generate the api domain once in the Railway UI and check it:

```sh
curl https://<generated-domain>/health/ready
```

## Redeploying

`source_hash` is an MD5 over `cmd/**/*.go`, `internal/**/*.go`, `go.mod`,
`go.sum`, both Dockerfiles and both `railway.*.json` files. Change any of them
and `terraform apply` re-uploads both services. Editing a variable in
`terraform.tfvars` also triggers a redeploy — `railway_variable_collection`
redeploys the service on change.

To force a redeploy without a code change:

```sh
terraform apply -replace='module.api.terraform_data.deploy'
```

`deploy_flag` defaults to `--ci`, so `apply` blocks until the build finishes and
fails if the build fails. Set it to `--detach` for fire-and-forget.

## Notes

- `terraform.tfstate` holds `db_url` and `railway_token` in plaintext. It is
  gitignored; keep it off shared machines.
- **State is what makes the project reusable.** This env keeps state in
  `terraform.tfstate` next to these files. Lose that file and the next `apply`
  creates a *brand new* Railway project instead of adopting the existing one —
  that is how a duplicate `olx-scraper-local` appeared and ate the Free plan
  quota. Recover with `terraform import railway_project.this <project_id>`
  before applying, never by applying on empty state. `production` is immune:
  its state lives in HCP Terraform (`cloud {}` in `versions.tf`).
- Build config (Dockerfile path, healthcheck, restart policy) lives in
  `railway.api.json` / `railway.worker.json` at the repo root — the provider has
  no fields for them.
- `.railwayignore` keeps `infra/`, `.idea/`, `.serena/` and docs out of the
  upload; `.gitignore` already excludes `.env` and `*.db`.
- Free trial: 5 projects, 5 services per project, 1 GB RAM, shared vCPU. The $5
  credit lasts 30 days; after that the Free plan's $1/month will not keep two
  always-on services running.
