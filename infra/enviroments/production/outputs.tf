output "project_id" {
  value       = railway_project.this.id
  description = "Identifier of the Railway project."
}

output "environment_id" {
  value       = railway_project.this.default_environment.id
  description = "Identifier of the default environment."
}

output "worker_service_id" {
  value       = module.worker.service_id
  description = "Identifier of the worker service."
}
