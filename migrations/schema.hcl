enum "product_type" {
    schema = schema.public
    values = ["RAW_MATERIAL", "PACKAGING", "FINISHED_GOOD", "WHITE_LABEL"]
}

enum "batch_status" {
    schema = schema.public
    values = ["ACTIVE", "DEPLETED", "QUARANTINED", "EXPIRED", "RESERVED"]
}

enum "transaction_type" {
    schema = schema.public
    values = ["INCOMING", "OUTGOING", "USED_IN_PRODUCTION", "WASTE", "ADJUSTMENT"]
}

enum "production_order_status" {
    schema = schema.public
    values = ["PLANNED", "IN_PROGRESS", "COMPLETED", "CANCELLED"]
}

enum "user_role" {
    schema = schema.public
    values = ["ADMIN", "OPERATOR", "VIEWER"]
}

schema "public" {
}

table "products" {
    schema = schema.public

    column "id" {
        null = false
        type = uuid
        default =sql("gen_random_uuid()")
    }

    column "sku" {
        null = false
        type = varchar(100)
    }

    column "name" {
        null = false
        type = varchar(200)
    }

    column "unit_of_measure" {
        null = false
        type = varchar(20)    
    }

    column "product_type" {
        null = false
        type = enum.product_type
    }

    column "is_purchasable" { 
        null = false
        type = boolean
        default = true 
    }
    column "is_sellable" { 
        null = false
        type = boolean
        default = false 
    }
    column "created_at" { 
        null = false
        type = timestamptz
        default = sql("now()") 
    }
    column "updated_at" { 
        null = false
        type = timestamptz
        default =sql("now()") 
    }

    column "deleted_at" {
        null = true
        type = timestamptz
    }

    primary_key { columns = [column.id] }
    index "products_sku_key" { 
        unique = true
        columns = [column.sku] 
    }
}

table "inventory_batches" {
    schema = schema.public

    column "id" {
        null = false
        type = uuid
        default = sql("gen_random_uuid()")
    }

    column "product_id" {
        null = false
        type = uuid
    }

    column "batch_number" {
        null = false
        type = varchar(100)
    }

    column "quantity_initial" {
        null = false
        type = decimal(10, 4)
    }

    column "quantity_current" {
        null = false
        type = decimal(10, 4)
    }

    column "status" {
        null = false
        type = enum.batch_status
    }

    column "expiration_date" {
        null = true
        type = timestamptz
    }

    column "created_at" { 
        null = false
        type = timestamptz
        default = sql("now()") 
    }
    column "updated_at" { 
        null = false
        type = timestamptz
        default = sql("now()") 
    }

    primary_key { columns = [column.id] }
    index "inventory_batches_batch_number_idx" { columns = [column.batch_number] }
    index "inventory_batches_product_id_idx" {columns = [column.product_id] }
    foreign_key "inventory_batches_product_fk" {
        columns = [column.product_id]
        ref_columns = [table.products.column.id]
        on_delete = RESTRICT
    }
    check "inventory_batches_quantity_initial_positive" {
        expr = "quantity_initial > 0"
    }
    check "inventory_batches_quantity_current_non_negative" {
        expr = "quantity_current >= 0"
    }
}

table "stock_transactions" {
    schema = schema.public

    column "id" {
        null = false
        type = uuid
        default = sql("gen_random_uuid()")
    }

    column "batch_id" {
        null = false
        type = uuid
    }

    column "production_order_id" {
        null = true
        type = uuid
    }

    column "quantity_change" {
        null = false
        type = decimal(10, 4)
    }

    column "transaction_type" {
        null = false
        type = enum.transaction_type
        default = "INCOMING"
    }

    column "performed_by" {
        null = false
        type = uuid
    }

    column "reference_note" {
        null = true
        type = varchar(500)
    }

    column "created_at" { 
        null = false
        type = timestamptz
        default = sql("now()") 
    }

    primary_key { columns = [column.id] }
    index "stock_transactions_batch_id_idx" { columns = [column.batch_id] }
    index "stock_transactions_production_order_id_idx" { columns = [column.production_order_id] }
    index "stock_transactions_performed_by_idx" { columns = [column.performed_by] }
    foreign_key "stock_transactions_batch_fk" {
        columns = [column.batch_id]
        ref_columns = [table.inventory_batches.column.id]
        on_delete = RESTRICT
    }
    foreign_key "stock_transactions_production_order_fk" {
        columns = [column.production_order_id]
        ref_columns = [table.production_orders.column.id]
        on_delete = RESTRICT
    }
    foreign_key "stock_transactions_performed_by_fk" {
        columns = [column.performed_by]
        ref_columns = [table.users.column.id]
        on_delete = RESTRICT
    }
    check "stock_transactions_incoming_quantity_positive" {
        expr = "quantity_change > 0 OR transaction_type <> 'INCOMING'"
    }
}

table "production_orders" {
    schema = schema.public
    
    column "id" {
        null = false
        type = uuid
        default = sql("gen_random_uuid()")
    }

    column "output_batch_id" {
        null = false
        type = uuid
    }

    column "status" {
        null = false
        type = enum.production_order_status
        default = "PLANNED"
    }

    column "created_by" {
        null = false
        type = uuid
    }

    column "created_at" { 
        null = false
        type = timestamptz
        default = sql("now()") 
    }
    column "completed_at" { 
        null = true
        type = timestamptz 
    }

    primary_key { columns = [column.id] }
    index "production_orders_output_batch_id_idx" { columns = [column.output_batch_id] }
    index "production_orders_created_by_idx" { columns = [column.created_by] }
    foreign_key "production_orders_output_batch_fk" {
        columns = [column.output_batch_id]
        ref_columns = [table.inventory_batches.column.id]
        on_delete = RESTRICT
    }
    foreign_key "production_orders_created_by_fk" {
        columns = [column.created_by]
        ref_columns = [table.users.column.id]
        on_delete = RESTRICT
    }
}

table "production_order_line_items" {
    schema = schema.public

    column "id" {
        null = false
        type = uuid
        default = sql("gen_random_uuid()")
    }

    column "production_order_id" {
        null = false
        type = uuid
    }

    column "input_batch_id" {
        null = false
        type = uuid
    }

    column "quantity_consumed" {
        null = false
        type = decimal(10, 4)
    }

    column "created_at" { 
        null = false
        type = timestamptz
        default = sql("now()") 
    }

    primary_key { columns = [column.id] }
    index "line_items_production_order_id_idx" { columns = [column.production_order_id] }
    index "line_items_input_batch_id_idx" { columns = [column.input_batch_id] }
    foreign_key "line_items_production_order_fk" {
        columns = [column.production_order_id]
        ref_columns =  [table.production_orders.column.id]
        on_delete = RESTRICT
    }
    foreign_key "line_items_input_batch_id_fk" {
        columns = [column.input_batch_id]
        ref_columns = [table.inventory_batches.column.id]
        on_delete = RESTRICT
    }
}

table "users" {
    schema = schema.public

    column "id" {
        null = false
        type = uuid
        default = sql("gen_random_uuid()")
    }

    column "username" {
        null = false
        type = varchar(100)
    }

    column "email" {
        null = false
        type = varchar(255)
    }

    column "password_hash" {
        null = false
        type = varchar(255)
    }

    column "role" {
        null = false
        type = enum.user_role
    }

    column "is_active" {
        null = false
        type = boolean
        default = true
    }

    column "created_at" { 
        null = false
        type = timestamptz
        default = sql("now()") 
    }
    column "updated_at" { 
        null = false
        type = timestamptz
        default = sql("now()") 
    }

    primary_key { columns = [column.id] }
    index "users_username_key" { 
        unique = true
        columns = [column.username] 
    }
    index "users_email_key" { 
        unique = true
        columns = [column.email] 
    }
}

table "refresh_tokens" {
    schema = schema.public

    column "id" {
        null = false
        type = uuid
        default = sql("gen_random_uuid()")
    }

    column "user_id" {
        null = false
        type = uuid
    }

    column "token_hash" {
        null = false
        type = varchar(64)
    }

    column "expires_at" {
        null = false
        type = timestamptz
    }

    column "revoked" { 
        null = false
        type = boolean
        default = false 
    }
    column "created_at" { 
        null = false
        type=timestamptz
        default=sql("now()") 
    }

    primary_key { columns = [column.id] }
    index "refresh_tokens_user_id_idx" { columns = [column.user_id] }
    index "refresh_tokens_token_hash_idx" {
        unique = true
        columns = [column.token_hash]
    }
    foreign_key "refresh_tokens_users_fk" {
        columns = [column.user_id]
        ref_columns = [table.users.column.id]
        on_delete = CASCADE
    }
}