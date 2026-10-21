-- Gemini / Antigravity gates now enforce the saved administrator setting.
-- Keep the upstream migration slot, but never enable existing groups implicitly.
-- New-group defaults are owned by group creation, not an inventory-wide update.
SELECT 1;
