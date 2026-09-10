-- Seed fixtures for tests/integration.
--
-- Every row uses a fixed UUID so tests can reference it directly, and the
-- password hash below is bcrypt("Password123!") so tests can log in through
-- the real POST /api/v1/auth/login endpoint.
--
-- Always call testutil.ResetDB before testutil.LoadSeed: the fixtures are
-- inserted unconditionally and rely on a clean schema.

INSERT INTO users (id, username, email, password_hash, role) VALUES
    ('00000000-0000-0000-0000-000000000001', 'seed-admin',    'seed-admin@example.com',    '$2a$10$s6gL.sj8WMGLvzJ28n9z8O107TXhVNgt3ibxoJAbEC2pv1rkUFHy.', 'ADMIN'),
    ('00000000-0000-0000-0000-000000000002', 'seed-operator', 'seed-operator@example.com', '$2a$10$s6gL.sj8WMGLvzJ28n9z8O107TXhVNgt3ibxoJAbEC2pv1rkUFHy.', 'OPERATOR'),
    ('00000000-0000-0000-0000-000000000003', 'seed-viewer',   'seed-viewer@example.com',   '$2a$10$s6gL.sj8WMGLvzJ28n9z8O107TXhVNgt3ibxoJAbEC2pv1rkUFHy.', 'VIEWER');

INSERT INTO products (id, sku, name, unit_of_measure, product_type, is_purchasable, is_sellable) VALUES
    ('10000000-0000-0000-0000-000000000001', 'SEED-RM-001',  'Seed Wheat Flour',     'kg',   'RAW_MATERIAL', TRUE,  FALSE),
    ('10000000-0000-0000-0000-000000000002', 'SEED-PKG-001', 'Seed Bread Box',       'pcs',  'PACKAGING',    TRUE,  FALSE),
    ('10000000-0000-0000-0000-000000000003', 'SEED-WL-001',  'Seed Unbranded Loaf',  'pack', 'WHITE_LABEL',  FALSE, FALSE),
    ('10000000-0000-0000-0000-000000000004', 'SEED-FG-001',  'Seed Branded Loaf',    'pack', 'FINISHED_GOOD', FALSE, TRUE);

-- Raw-material batches with staggered expirations plus one NULL expiration so
-- FEFO tests can prove `expiration_date ASC NULLS LAST`.
INSERT INTO inventory_batches (id, product_id, batch_number, quantity_initial, quantity_current, status, expiration_date) VALUES
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'SEED-RM-OLD',  10.0000, 10.0000, 'ACTIVE', '2026-10-01T00:00:00Z'),
    ('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'SEED-RM-MID',  10.0000, 10.0000, 'ACTIVE', '2026-12-01T00:00:00Z'),
    ('20000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', 'SEED-RM-NEW',  10.0000, 10.0000, 'ACTIVE', '2027-03-01T00:00:00Z'),
    ('20000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', 'SEED-RM-NULL', 10.0000, 10.0000, 'ACTIVE', NULL);

-- White-label input batches for production orders. The oldest expiration is
-- 2026-11-15, so completing an order that consumes both must yield an output
-- batch expiring exactly then.
INSERT INTO inventory_batches (id, product_id, batch_number, quantity_initial, quantity_current, status, expiration_date) VALUES
    ('21000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000003', 'SEED-WL-OLD', 5.0000, 5.0000, 'ACTIVE', '2026-11-15T00:00:00Z'),
    ('21000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000003', 'SEED-WL-NEW', 5.0000, 5.0000, 'ACTIVE', '2027-02-15T00:00:00Z');
