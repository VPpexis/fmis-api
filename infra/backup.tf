data "aws_iam_policy_document" "dlm_assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["dlm.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "dlm" {
  name               = "${var.project}-dlm"
  assume_role_policy = data.aws_iam_policy_document.dlm_assume.json
  tags               = local.common_tags
}

resource "aws_iam_role_policy_attachment" "dlm" {
  role       = aws_iam_role.dlm.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSDataLifecycleManagerServiceRole"
}

resource "aws_dlm_lifecycle_policy" "postgres_data" {
  description        = "Daily EBS snapshots for the ${var.project} PostgreSQL data volume"
  state              = "ENABLED"
  execution_role_arn = aws_iam_role.dlm.arn

  policy_details {
    resource_types = ["VOLUME"]

    target_tags = {
      Snapshot = "daily"
    }

    schedule {
      name = "daily-${var.backup_retention_days}d"

      create_rule {
        interval      = 24
        interval_unit = "HOURS"
        times         = ["02:00"]
      }

      retain_rule {
        count = var.backup_retention_days
      }

      copy_tags = true
    }
  }

  tags = local.common_tags
}
