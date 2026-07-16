-- name: CreateEmployee :one
INSERT INTO employees (
  tenant_id, employee_no, full_name, phone, role,
  license_no, license_class, license_expiry, status
) VALUES (
  $1, sqlc.narg('employee_no'), $2, sqlc.narg('phone'), COALESCE(sqlc.narg('role'), 'driver'),
  sqlc.narg('license_no'), sqlc.narg('license_class'), sqlc.narg('license_expiry'),
  COALESCE(sqlc.narg('status'), 'active')
)
RETURNING *;

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
