-- Modify "products" table
ALTER TABLE "public"."products" ADD COLUMN "deleted_at" timestamptz NULL;
