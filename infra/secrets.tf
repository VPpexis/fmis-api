resource "random_password" "db" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>:?"
}

resource "aws_secretsmanager_secret" "db" {
  name                    = "${var.project}/db"
  description             = "PostgreSQL credentials for the ${var.project} PoC database."
  recovery_window_in_days = 0

  tags = merge(local.common_tags, { Name = "${var.project}-db" })
}

resource "aws_secretsmanager_secret_version" "db" {
  secret_id = aws_secretsmanager_secret.db.id

  secret_string = jsonencode({
    username = var.db_username
    password = random_password.db.result
    engine   = "postgres"
    port     = 5432
    dbname   = var.db_name
  })
}
