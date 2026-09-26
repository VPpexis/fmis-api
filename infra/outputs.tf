output "ecr_repository_url" {
  value = aws_ecr_repository.app.repository_url
}

output "github_actions_role_arn" {
  value = aws_iam_role.github_actions.arn
}

output "db_instance_id" {
  value = aws_instance.postgres.id
}

output "db_private_ip" {
  value = aws_instance.postgres.private_ip
}

output "db_security_group_id" {
  value = aws_security_group.db.id
}

output "app_tier_security_group_id" {
  value = aws_security_group.app_tier.id
}

output "db_secret_arn" {
  value = aws_secretsmanager_secret.db.arn
}
