terraform {
  required_version = ">= 1.6"

  required_providers {
    railway = {
      source  = "terraform-community-providers/railway"
      version = "~> 0.6"
    }
  }
}

resource "railway_service" "this" {
  name       = var.service_name
  project_id = var.project_id

  regions = [{
    region       = var.region
    num_replicas = var.num_replicas
  }]
}

resource "railway_variable_collection" "this" {
  environment_id = var.environment_id
  service_id     = railway_service.this.id

  variables = [for name, value in var.variables : {
    name  = name
    value = value
  }]
}

# Uploads the repository from this machine and lets Railway build the Dockerfile.
# No GitHub connection is involved.
resource "terraform_data" "deploy" {
  triggers_replace = [var.source_hash, railway_service.this.id]

  provisioner "local-exec" {
    working_dir = var.repo_root
    environment = { RAILWAY_API_TOKEN = var.railway_token }
    command     = "railway up --project ${var.project_id} --environment ${var.environment_id} --service ${railway_service.this.id} ${var.deploy_flag}"
  }

  # Variables must exist before the first boot, otherwise the process exits on
  # "DB_URL is required" (internal/config/config.go).
  depends_on = [railway_variable_collection.this]
}
