-- 0003_employees.sql — driver/staff registry.
--
-- Employees are the people operating the fleet (drivers, mechanics, staff),
-- distinct from `users` (login accounts). A vehicle's current_driver_id points
-- here (0004).

CREATE TABLE employees (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint NOT NULL REFERENCES tenants(id),
  employee_no   text,                                   -- HR / employee id
  full_name     text NOT NULL,
  phone         text,
  role          text NOT NULL DEFAULT 'driver'
                CHECK (role IN ('driver','staff','mechanic')),
  license_no    text,                                   -- SIM number
  license_class text,                                   -- SIM A / B1 / B2 ...
  license_expiry date,
  status        text NOT NULL DEFAULT 'active'
                CHECK (status IN ('active','inactive')),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, employee_no)
);
CREATE INDEX idx_employees_tenant ON employees (tenant_id);
CREATE TRIGGER trg_employees_touch BEFORE UPDATE ON employees
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
