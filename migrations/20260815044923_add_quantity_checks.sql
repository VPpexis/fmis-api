-- Modify "inventory_batches" table
ALTER TABLE "public"."inventory_batches" ADD CONSTRAINT "inventory_batches_quantity_current_non_negative" CHECK (quantity_current >= (0)::numeric), ADD CONSTRAINT "inventory_batches_quantity_initial_positive" CHECK (quantity_initial > (0)::numeric);
-- Modify "stock_transactions" table
ALTER TABLE "public"."stock_transactions" ADD CONSTRAINT "stock_transactions_incoming_quantity_positive" CHECK ((quantity_change > (0)::numeric) OR (transaction_type <> 'INCOMING'::public.transaction_type));
