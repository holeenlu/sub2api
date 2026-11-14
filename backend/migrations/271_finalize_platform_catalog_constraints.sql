-- Historical fork migrations after upstream 242 recreate these whitelists.
-- Finalize application-owned platform validation after those immutable files,
-- for both existing databases and fresh installations. Monitor capability
-- constraints remain in place.
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
