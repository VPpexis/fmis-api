resource "aws_security_group" "app_tier" {
  name        = "${var.project}-app-tier"
  description = "Application tier (ECS Fargate / App Runner). Attach to compute in Phase 4."
  vpc_id      = data.aws_vpc.default.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(local.common_tags, { Name = "${var.project}-app-tier" })
}

resource "aws_security_group" "db" {
  name        = "${var.project}-db"
  description = "Self-managed PostgreSQL. Reachable only from the app tier security group."
  vpc_id      = data.aws_vpc.default.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(local.common_tags, { Name = "${var.project}-db" })
}

resource "aws_security_group_rule" "db_postgres_from_app" {
  type                     = "ingress"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  security_group_id        = aws_security_group.db.id
  source_security_group_id = aws_security_group.app_tier.id
  description              = "PostgreSQL only from the application tier."
}
