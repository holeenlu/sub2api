-- Zero means no vendor-local resource cap. Finite parent caps still apply in
-- the shared capacity check. Preserve every existing explicitly configured cap.
ALTER TABLE vendors
 DROP CONSTRAINT vendors_account_limit_check,
 DROP CONSTRAINT vendors_proxy_limit_check,
 ADD CONSTRAINT vendors_account_limit_check CHECK (account_limit BETWEEN 0 AND 10000),
 ADD CONSTRAINT vendors_proxy_limit_check CHECK (proxy_limit BETWEEN 0 AND 10000),
 ALTER COLUMN account_limit SET DEFAULT 0,
 ALTER COLUMN proxy_limit SET DEFAULT 0;
