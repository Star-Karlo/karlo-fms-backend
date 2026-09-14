-- name: CreateEmployee :one
-- master_data_id is set at insert, not patched afterwards, so a driver created
-- through the write-through path is born paired. A row that reaches the table
-- unpaired can only be fixed by a backfill, and until it is it cannot be
-- assigned to a vehicle — which is the whole failure this column exists to end.
INSERT INTO employees (
  tenant_id, employee_no, full_name, phone, role,
  license_no, license_class, license_expiry, status, master_data_id
) VALUES (
  $1, sqlc.narg('employee_no'), $2, sqlc.narg('phone'), COALESCE(sqlc.narg('role'), 'driver'),
  sqlc.narg('license_no'), sqlc.narg('license_class'), sqlc.narg('license_expiry'),
  COALESCE(sqlc.narg('status'), 'active'), sqlc.narg('master_data_id')
)
RETURNING *;

-- name: PairEmployee :one
-- Attach an existing local driver to its master-data record. Used by the
-- backfill for rows created before write-through existed. Only ever fills a
-- NULL: re-pointing a driver that is already paired would silently move every
-- vehicle assignment that references it onto a different person.
UPDATE employees SET master_data_id = $2, updated_at = now()
 WHERE id = $1 AND master_data_id IS NULL
RETURNING *;

-- name: ListUnpairedEmployees :many
-- Local drivers with no master-data counterpart, oldest first.
SELECT * FROM employees
 WHERE tenant_id = $1 AND master_data_id IS NULL
 ORDER BY id;

-- name: GetEmployee :one
SELECT * FROM employees WHERE id = $1;

-- name: UpdateEmployee :one
UPDATE employees
SET employee_no    = COALESCE(sqlc.narg('employee_no'), employee_no),
    full_name      = COALESCE(sqlc.narg('full_name'), full_name),
    phone          = COALESCE(sqlc.narg('phone'), phone),
    role           = COALESCE(sqlc.narg('role'), role),
    license_no     = COALESCE(sqlc.narg('license_no'), license_no),
    license_class  = COALESCE(sqlc.narg('license_class'), license_class),
    license_expiry = COALESCE(sqlc.narg('license_expiry'), license_expiry),
    status         = COALESCE(sqlc.narg('status'), status)
WHERE id = $1
RETURNING *;

-- name: DeleteEmployee :exec
DELETE FROM employees WHERE id = $1;

-- name: ListEmployees :many
SELECT * FROM employees
WHERE (sqlc.narg('q')::text IS NULL
       OR full_name ILIKE '%' || sqlc.narg('q') || '%'
       OR employee_no ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY full_name
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountEmployees :one
SELECT count(*) FROM employees
WHERE (sqlc.narg('q')::text IS NULL
       OR full_name ILIKE '%' || sqlc.narg('q') || '%'
       OR employee_no ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'));
