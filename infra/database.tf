data "aws_iam_policy_document" "ec2_assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "postgres" {
  name               = "${var.project}-postgres"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
  tags               = local.common_tags
}

resource "aws_iam_role_policy_attachment" "postgres_ssm" {
  role       = aws_iam_role.postgres.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

data "aws_iam_policy_document" "postgres_secret_read" {
  statement {
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.db.arn]
  }
}

resource "aws_iam_role_policy" "postgres_secret_read" {
  name   = "read-db-secret"
  role   = aws_iam_role.postgres.id
  policy = data.aws_iam_policy_document.postgres_secret_read.json
}

resource "aws_iam_instance_profile" "postgres" {
  name = "${var.project}-postgres"
  role = aws_iam_role.postgres.name
  tags = local.common_tags
}

resource "aws_ebs_volume" "postgres_data" {
  availability_zone = data.aws_subnet.db.availability_zone
  size              = var.db_volume_size
  type              = "gp3"
  encrypted         = true

  tags = merge(local.common_tags, {
    Name     = "${var.project}-postgres-data"
    Snapshot = "daily"
  })

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_instance" "postgres" {
  ami                         = data.aws_ami.al2023.id
  instance_type               = var.db_instance_type
  subnet_id                   = data.aws_subnet.db.id
  vpc_security_group_ids      = [aws_security_group.db.id]
  associate_public_ip_address = true
  iam_instance_profile        = aws_iam_instance_profile.postgres.name

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  root_block_device {
    volume_size = 8
    volume_type = "gp3"
    encrypted   = true
  }

  user_data = templatefile("${path.module}/user_data.sh.tftpl", {
    aws_region        = var.aws_region
    secret_arn        = aws_secretsmanager_secret.db.arn
    db_name           = var.db_name
    db_username       = var.db_username
    db_engine_version = var.db_engine_version
    vpc_cidr          = data.aws_vpc.default.cidr_block
    data_device       = "/dev/sdf"
    data_mount        = "/var/lib/pgsql"
  })

  user_data_replace_on_change = true

  tags = merge(local.common_tags, { Name = "${var.project}-postgres" })

  depends_on = [
    aws_secretsmanager_secret_version.db,
    aws_iam_role_policy.postgres_secret_read,
  ]
}

resource "aws_volume_attachment" "postgres_data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.postgres_data.id
  instance_id = aws_instance.postgres.id
}
