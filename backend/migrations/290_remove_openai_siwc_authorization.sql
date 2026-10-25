-- Remove the retired authorization on upgrade, including disconnected
-- registrations and soft-deleted accounts. Native and BPS credentials remain.
UPDATE accounts
SET extra = extra - 'openai_siwc'
WHERE jsonb_typeof(extra) = 'object' AND extra ? 'openai_siwc';

DROP TABLE IF EXISTS openai_siwc_grants;
DROP TABLE IF EXISTS openai_siwc_installation;

-- Historical usage classifications and settlement records are not credentials.
-- Keep their values, constraints and immutability guards unchanged.
