-- Optional DMARC aggregate-report destination (rua=) for the panel administrator
-- and per-domain overrides. NULL dmarc_rua on a domain means inherit the profile.

ALTER TABLE admin ADD COLUMN dmarc_report_email TEXT NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN dmarc_rua TEXT;
