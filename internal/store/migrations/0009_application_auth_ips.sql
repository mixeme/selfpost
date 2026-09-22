-- Client IP restriction for application SASL authorization (independent of
-- level-2 rate limits). When enabled, only listed IPs may submit mail as the
-- application; when disabled, any client IP is allowed.

ALTER TABLE applications ADD COLUMN auth_ip_restrict INTEGER NOT NULL DEFAULT 0;
ALTER TABLE applications ADD COLUMN auth_allowed_ips TEXT;

-- Migrate trusted IPs from application rate_limits into auth_allowed_ips.
UPDATE applications
SET auth_ip_restrict = 1,
    auth_allowed_ips = (
        SELECT allowed_ips FROM rate_limits
        WHERE scope = 'application' AND ref_id = applications.id
    )
WHERE EXISTS (
    SELECT 1 FROM rate_limits
    WHERE scope = 'application' AND ref_id = applications.id
      AND allowed_ips IS NOT NULL AND TRIM(allowed_ips) != ''
);

UPDATE rate_limits SET allowed_ips = NULL
WHERE scope = 'application' AND allowed_ips IS NOT NULL;
