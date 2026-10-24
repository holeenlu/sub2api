-- Global manual synchronization has no single owning account. Keep its job
-- status in the shared store so polling also works behind a load balancer.
ALTER TABLE model_catalog_jobs ALTER COLUMN account_id DROP NOT NULL;
