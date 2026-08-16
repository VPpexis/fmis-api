env "local" {
    url = "postgres://fmis:fmis_dev@localhost:5432/fmis_db?sslmode=disable"
    dev = "docker://postgres/16/dev"
    src = "file://migrations/schema.hcl"
    migration {
        dir = "file://migrations"
    }
}
env "production" {
    url = "${PROD_DATABASE_URL}"
    dev = "docker://postgres/16/dev"
    src = "file://migrations/schema.hcl"
    migration {
        dir = "file://migrations"
    }
}