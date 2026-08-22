terraform {
  required_version = ">= 1.6"

  # State and locking live in HCP Terraform; the run itself happens on the
  # GitHub Actions runner, so the workspace must be set to Execution Mode =
  # Local — HCP's own workers have no `railway` CLI for terraform_data.deploy.
  #
  # Organization and workspace come from TF_CLOUD_ORGANIZATION and TF_WORKSPACE
  # so the repository carries no account-specific names.
  cloud {}

  required_providers {
    railway = {
      source  = "terraform-community-providers/railway"
      version = "~> 0.6"
    }
  }
}

provider "railway" {
  token = var.railway_token
}
