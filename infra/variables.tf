variable "aws_region" {
  description = "AWS region for all resources."
  type        = string
  default     = "ap-southeast-1"
}

variable "project" {
  description = "Project name used for resource naming."
  type        = string
  default     = "fmis-api"
}

variable "github_repository" {
  description = "GitHub repository (owner/name) allowed to assume the role."
  type        = string
  default     = "VPpexis/fmis-api"
}

variable "github_owner_id" {
  description = "Numeric GitHub owner ID, embedded in immutable OIDC subject claims."
  type        = string
  default     = "42709770"
}

variable "github_repository_id" {
  description = "Numeric GitHub repository ID, embedded in immutable OIDC subject claims."
  type        = string
  default     = "1316228767"
}

variable "db_instance_type" {
  description = "EC2 instance type for the self-managed PostgreSQL host."
  type        = string
  default     = "t3.micro"
}

variable "db_volume_size" {
  description = "Size in GiB of the encrypted gp3 EBS data volume for PostgreSQL."
  type        = number
  default     = 20
}

variable "db_engine_version" {
  description = "PostgreSQL major version installed on the EC2 host."
  type        = string
  default     = "16"
}

variable "db_name" {
  description = "Application database created on the PostgreSQL instance."
  type        = string
  default     = "fmis_db"
}

variable "db_username" {
  description = "Application role created on the PostgreSQL instance."
  type        = string
  default     = "fmis"
}

variable "backup_retention_days" {
  description = "Number of daily EBS snapshots retained by the DLM policy."
  type        = number
  default     = 7
}
