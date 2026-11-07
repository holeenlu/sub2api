-- Existing supplier OpenAI admission supported only the native profile. Stop
-- for explicit reconciliation if nonstandard BPS supplier grants already exist.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='vendors' AND column_name='openai_authorization_modes') THEN RETURN; END IF;
 IF EXISTS (SELECT 1 FROM accounts a WHERE a.vendor_id IS NOT NULL AND a.platform='openai'
   AND (a.credentials->>'client_id'='app_fnr0pYvVwwFDocDumLG3H2Bp'
     OR EXISTS(SELECT 1 FROM openai_excel_grants g WHERE g.account_id=a.id AND g.ciphertext<>''))) THEN
   RAISE EXCEPTION 'Reconcile existing supplier BPS grants before applying authorization mode migration';
 END IF;
END $$;

ALTER TABLE vendors ADD COLUMN IF NOT EXISTS openai_authorization_modes JSONB NOT NULL DEFAULT '["native"]'::jsonb;
UPDATE vendors SET openai_authorization_modes='[]'::jsonb WHERE NOT (platforms ? 'openai');
DO $$ BEGIN
IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='vendors'::regclass AND conname='vendors_openai_authorization_modes_valid') THEN
ALTER TABLE vendors ADD CONSTRAINT vendors_openai_authorization_modes_valid CHECK (
 jsonb_typeof(openai_authorization_modes)='array'
 AND openai_authorization_modes <@ '["native","bps"]'::jsonb
 AND jsonb_array_length(openai_authorization_modes)<=2
);
END IF;
END $$;
