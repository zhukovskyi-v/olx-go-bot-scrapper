variable "token" {
  description = "Telegram bot token. Required; the process exits without it."
  type        = string
  sensitive   = true
}
locals {
  repo_root = abspath("${path.module}/../../..")

  # GitHub passes "" for an undefined repository variable; the provider wants null.
  workspace_id = var.workspace_id == "" ? null : var.workspace_id

  # Anything that ends up inside the image. A change here re-runs `railway up`.
  source_files = sort(concat(
    tolist(fileset(local.repo_root, "cmd/**/*.go")),
    tolist(fileset(local.repo_root, "internal/**/*.go")),
    [
      "go.mod",
      "go.sum",
      "Dockerfile",
      "railway.json",
    ],
  ))

  source_hash = md5(join("", [for f in local.source_files : filemd5("${local.repo_root}/${f}")]))

  env = {
    ENV    = var.env
    DB_URL = var.db_url
    TOKEN  = var.token
  }
}

resource "railway_project" "this" {
  name         = var.project_name
  description  = "OLX scraper bot"
  private      = true
  workspace_id = local.workspace_id

  default_environment = {
    name = var.environment_name
  }
}

module "worker" {
  source = "../../modules/worker"

  project_id     = railway_project.this.id
  environment_id = railway_project.this.default_environment.id
  region         = var.region
  variables      = local.env

  repo_root     = local.repo_root
  railway_token = var.railway_token
  source_hash   = local.source_hash
  deploy_flag   = var.deploy_flag
}
