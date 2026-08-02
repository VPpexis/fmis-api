-- Create enum type "product_type"
CREATE TYPE "public"."product_type" AS ENUM ('RAW_MATERIAL', 'PACKAGING', 'FINISHED_GOOD', 'WHITE_LABEL');
-- Create enum type "batch_status"
CREATE TYPE "public"."batch_status" AS ENUM ('ACTIVE', 'DEPLETED', 'QUARANTINED', 'EXPIRED');
-- Create enum type "transaction_type"
CREATE TYPE "public"."transaction_type" AS ENUM ('INCOMING', 'OUTGOING', 'USED_IN_PRODUCTION', 'WASTE', 'ADJUSTMENT');
-- Create enum type "production_order_status"
CREATE TYPE "public"."production_order_status" AS ENUM ('PLANNED', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED');
-- Create enum type "user_role"
CREATE TYPE "public"."user_role" AS ENUM ('ADMIN', 'OPERATOR', 'VIEWER');
-- Create "products" table
CREATE TABLE "public"."products" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "sku" character varying(100) NOT NULL,
  "name" character varying(200) NOT NULL,
  "unit_of_measure" character varying(20) NOT NULL,
  "product_type" "public"."product_type" NOT NULL,
  "is_purchasable" boolean NOT NULL DEFAULT true,
  "is_sellable" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "products_sku_key" to table: "products"
CREATE UNIQUE INDEX "products_sku_key" ON "public"."products" ("sku");
-- Create "inventory_batches" table
CREATE TABLE "public"."inventory_batches" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "product_id" uuid NOT NULL,
  "batch_number" character varying(100) NOT NULL,
  "quantity_initial" numeric(10,4) NOT NULL,
  "quantity_current" numeric(10,4) NOT NULL,
  "status" "public"."batch_status" NOT NULL,
  "expiration_date" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "inventory_batches_product_fk" FOREIGN KEY ("product_id") REFERENCES "public"."products" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "inventory_batches_batch_number_idx" to table: "inventory_batches"
CREATE INDEX "inventory_batches_batch_number_idx" ON "public"."inventory_batches" ("batch_number");
-- Create index "inventory_batches_product_id_idx" to table: "inventory_batches"
CREATE INDEX "inventory_batches_product_id_idx" ON "public"."inventory_batches" ("product_id");
-- Create "users" table
CREATE TABLE "public"."users" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "username" character varying(100) NOT NULL,
  "email" character varying(255) NOT NULL,
  "password_hash" character varying(255) NOT NULL,
  "role" "public"."user_role" NOT NULL,
  "is_active" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "users_email_key" to table: "users"
CREATE UNIQUE INDEX "users_email_key" ON "public"."users" ("email");
-- Create index "users_username_key" to table: "users"
CREATE UNIQUE INDEX "users_username_key" ON "public"."users" ("username");
-- Create "production_orders" table
CREATE TABLE "public"."production_orders" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "output_batch_id" uuid NOT NULL,
  "status" "public"."production_order_status" NOT NULL DEFAULT 'PLANNED',
  "created_by" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "completed_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "production_orders_created_by_fk" FOREIGN KEY ("created_by") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "production_orders_output_batch_fk" FOREIGN KEY ("output_batch_id") REFERENCES "public"."inventory_batches" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "production_orders_created_by_idx" to table: "production_orders"
CREATE INDEX "production_orders_created_by_idx" ON "public"."production_orders" ("created_by");
-- Create index "production_orders_output_batch_id_idx" to table: "production_orders"
CREATE INDEX "production_orders_output_batch_id_idx" ON "public"."production_orders" ("output_batch_id");
-- Create "production_order_line_items" table
CREATE TABLE "public"."production_order_line_items" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "production_order_id" uuid NOT NULL,
  "input_batch_id" uuid NOT NULL,
  "quantity_consumed" numeric(10,4) NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "line_items_input_batch_id_fk" FOREIGN KEY ("input_batch_id") REFERENCES "public"."inventory_batches" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "line_items_production_order_fk" FOREIGN KEY ("production_order_id") REFERENCES "public"."production_orders" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "line_items_input_batch_id_idx" to table: "production_order_line_items"
CREATE INDEX "line_items_input_batch_id_idx" ON "public"."production_order_line_items" ("input_batch_id");
-- Create index "line_items_production_order_id_idx" to table: "production_order_line_items"
CREATE INDEX "line_items_production_order_id_idx" ON "public"."production_order_line_items" ("production_order_id");
-- Create "refresh_tokens" table
CREATE TABLE "public"."refresh_tokens" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "token_hash" character varying(64) NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "revoked" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "refresh_tokens_users_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "refresh_tokens_user_id_idx" to table: "refresh_tokens"
CREATE INDEX "refresh_tokens_user_id_idx" ON "public"."refresh_tokens" ("user_id");
-- Create "stock_transactions" table
CREATE TABLE "public"."stock_transactions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "batch_id" uuid NOT NULL,
  "production_order_id" uuid NULL,
  "quantity_change" numeric(10,4) NOT NULL,
  "transaction_type" "public"."transaction_type" NOT NULL DEFAULT 'INCOMING',
  "performed_by" uuid NOT NULL,
  "reference_note" character varying(500) NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "stock_transactions_batch_fk" FOREIGN KEY ("batch_id") REFERENCES "public"."inventory_batches" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "stock_transactions_performed_by_fk" FOREIGN KEY ("performed_by") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "stock_transactions_production_order_fk" FOREIGN KEY ("production_order_id") REFERENCES "public"."production_orders" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT
);
-- Create index "stock_transactions_batch_id_idx" to table: "stock_transactions"
CREATE INDEX "stock_transactions_batch_id_idx" ON "public"."stock_transactions" ("batch_id");
-- Create index "stock_transactions_performed_by_idx" to table: "stock_transactions"
CREATE INDEX "stock_transactions_performed_by_idx" ON "public"."stock_transactions" ("performed_by");
-- Create index "stock_transactions_production_order_id_idx" to table: "stock_transactions"
CREATE INDEX "stock_transactions_production_order_id_idx" ON "public"."stock_transactions" ("production_order_id");
