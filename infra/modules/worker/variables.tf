variable "project_id" {
  type        = string
  description = "Identifier of the Railway project the service belongs to."
}

variable "environment_id" {
  type        = string
  description = "Identifier of the Railway environment the service is deployed into."
}

variable "service_name" {
  type        = string
  default     = "worker"
  description = "Name of the Railway service."
}

variable "config_path" {
  type        = string
  default     = "/railway.json"
  description = "Absolute path (inside the uploaded source) to the Railway config file."
}

variable "region" {
  type        = string
  default     = "europe-west4"
  description = "Railway region to deploy replicas in."
}

variable "num_replicas" {
  type        = number
  default     = 1
  description = "Number of replicas to run in the region. Keep at 1 — the discovery and refresh loops are not sharded across processes."

  validation {
    condition     = var.num_replicas == 1
    error_message = "worker must run a single replica, otherwise every replica sweeps every city."
  }
}

variable "variables" {
  type        = map(string)
  sensitive   = true
  description = "Environment variables set on the service."
}

variable "repo_root" {
  type        = string
  description = "Absolute path to the repository root uploaded by `railway up`."
}

variable "railway_token" {
  type        = string
  sensitive   = true
  description = "Account-scoped Railway token passed to the CLI as RAILWAY_API_TOKEN."
}

variable "source_hash" {
  type        = string
  description = "Hash of the deployable sources. Changing it re-runs `railway up`."
}

variable "deploy_flag" {
  type        = string
  default     = "--ci"
  description = "Mode flag for `railway up`: --ci waits for the build, --detach returns immediately."

  validation {
    condition     = contains(["--ci", "--detach"], var.deploy_flag)
    error_message = "deploy_flag must be --ci or --detach."
  }
}
