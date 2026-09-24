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
