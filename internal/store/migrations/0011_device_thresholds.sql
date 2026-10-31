-- Thresholds that admins set for one device, in place of the settings'. A
-- column is NULL to use the setting's.
ALTER TABLE device_settings ADD COLUMN disk_warn       REAL;    -- percent
ALTER TABLE device_settings ADD COLUMN disk_critical   REAL;    -- percent
ALTER TABLE device_settings ADD COLUMN memory_warn     REAL;    -- percent
ALTER TABLE device_settings ADD COLUMN cpu_warn        REAL;    -- percent
ALTER TABLE device_settings ADD COLUMN offline_minutes INTEGER;
