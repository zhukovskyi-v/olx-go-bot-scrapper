# production — Railway deploy from GitHub Actions

Same two services as `../local` (`api`, `worker`), but:

- state and locking live in an **HCP Terraform** workspace;
- the run happens on a GitHub Actions runner, not on a workstation;
- variables come from GitHub secrets/variables as `TF_VAR_*`.

Railway still has no access to GitHub — Actions pushes code to Railway with
`railway up`, nothing pulls from the repo.

## One-time setup

1. **HCP Terraform** (app.terraform.io): create an organization, then a
   **CLI-driven** workspace, e.g. `olx-scraper-production`.
2. Workspace → Settings → General → **Execution Mode = Local (custom)**.
   Mandatory: HCP's own workers have no `railway` CLI, so a remote run would
   fail inside `terraform_data.deploy` after creating half the resources.
   Local mode also means HCP does *not* evaluate workspace variables — every
   value must arrive as `TF_VAR_*`.
3. User Settings → Tokens → create an API token → GitHub secret `TF_API_TOKEN`.
4. Create the GitHub secrets and variables below.

### GitHub repository secrets

| Name                | Value                                              |
|---------------------|----------------------------------------------------|
| `TF_API_TOKEN`      | HCP Terraform user token                           |
| `RAILWAY_API_TOKEN` | Railway account token (railway.com/account/tokens) |
| `DB_URL`            | Turso DSN for production                           |
| `SENTRY_DSN`        | may be empty                                       |

### GitHub repository variables

| Name                    | Value                                                 |
|-------------------------|-------------------------------------------------------|
| `TF_CLOUD_ORGANIZATION` | HCP organization name                                 |
| `TF_WORKSPACE`          | HCP workspace name, e.g. `olx-scraper-production`     |
| `REFRESH_PROXY_URL`     | proxy endpoint for detail refreshes                   |
| `RAILWAY_WORKSPACE_ID`  | optional; leave unset with a single Railway workspace |

The `cloud {}` block is empty on purpose — `TF_CLOUD_ORGANIZATION` and
`TF_WORKSPACE` fill it in, so no account-specific name is committed.

Everything else (`region`, intervals, `rate_limit_rpm`, `fx_sync_hour`) uses the
defaults in `variables.tf`.

## Workflow

`.github/workflows/deploy.yml`:

- **pull request** → `fmt -check`, `init`, `validate`, `plan`, plan posted as a
  PR comment. No apply.
- **push to main** (paths: `cmd/`, `internal/`, `go.*`, `Dockerfile.*`,
  `railway.*.json`, `infra/`, the workflow itself) → plan + `apply`.
- **workflow_dispatch** → same as a push.

Parallel deploys are blocked three times over: the HCP workspace lock, the
`concurrency: railway-production` group, and the HCP free tier's single
concurrent run.

## Manual run from a workstation

```sh
export TF_CLOUD_ORGANIZATION=<org>
export TF_WORKSPACE=olx-scraper-production
terraform login          # or export TF_TOKEN_app_terraform_io=<token>
cp terraform.tfvars.example terraform.tfvars
terraform init
terraform plan
```

Requires the `railway` CLI locally (`brew install railway`), same as `../local`.

## Notes

- The api's public domain is not managed here — generate it once in the Railway
  UI (Settings → Networking).
- `source_hash` is an MD5 over the Go sources, `go.mod`/`go.sum`, both
  Dockerfiles and both `railway.*.json`. Unchanged hash → `railway up` does not
  re-run.
- This is a **second** Railway project alongside `olx-scraper-local`: four
  services total, so the trial credit burns twice as fast. Point it at its own
  Turso database, or destroy `../local` — two workers on one database scrape and
  write the same rows.
