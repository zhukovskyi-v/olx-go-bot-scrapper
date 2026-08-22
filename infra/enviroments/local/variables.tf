variable "railway_token" {
  type        = string
  sensitive   = true
  description = "Account-scoped Railway token (railway.com/account/tokens). Used by both the provider and the CLI."
}

variable "workspace_id" {
  type        = string
  default     = null
  description = "Railway workspace the project belongs to. Required — the token does not infer it, and projectCreate rejects a request without it."
}

variable "project_name" {
  type        = string
  default     = "olx-scraper-local"
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
