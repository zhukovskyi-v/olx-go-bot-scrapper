variable "railway_token" {
  type        = string
  sensitive   = true
  description = "Account-scoped Railway token (railway.com/account/tokens). Used by both the provider and the CLI."
}

variable "workspace_id" {
  type        = string
  default     = null
  description = "Railway workspace the project belongs to. Leave unset — it is discovered from railway_token. Set it only to pin a specific workspace and skip the lookup."
}

variable "project_name" {
  type        = string
  default     = "olx scraper production"
  description = "Name of the Railway project."
}

variable "environment_name" {
  type        = string
  default     = "production"
  description = "Name of the project's default environment."
}

variable "region" {
  type        = string
  default     = "europe-west4"
  description = "Railway region for both services."
}

variable "deploy_flag" {
  type        = string
  default     = "--ci"
  description = "`railway up` mode: --ci blocks until the build finishes, --detach returns immediately."
}

# --- application configuration (internal/config/config.go) ---

variable "env" {
  type        = string
  default     = "prod"
  description = "ENV value reported to Sentry and used for logging setup."
}

variable "db_url" {
  type        = string
  sensitive   = true
  description = "DB_URL — database DSN. libsql:// take the Turso db path. Required; the process exits without it."

  validation {
    condition     = length(trimspace(var.db_url)) > 0
    error_message = "db_url must not be empty."
  }
}
