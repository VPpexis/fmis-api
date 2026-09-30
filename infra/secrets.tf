resource "random_password" "db" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>:?"
}

resource "aws_ssm_parameter" "db" {
  name = "/${var.project}/db"
  type = "SecureString"
  value = jsonencode({
    username = var.db_username
    password = random_password.db.result
    engine   = "postgres"
    port     = 5432
    dbname   = var.db_name
  })

  tags = merge(local.common_tags, { Name = "${var.project}-db" })
}
