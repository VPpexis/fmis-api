-- Create index "refresh_tokens_token_hash_idx" to table: "refresh_tokens"
CREATE UNIQUE INDEX "refresh_tokens_token_hash_idx" ON "public"."refresh_tokens" ("token_hash");
