-- name: GetUserDashboard :one
SELECT layout FROM dashboard_layouts
WHERE tenant_id = @tenant_id AND user_id = @user_id;

-- name: UpsertUserDashboard :exec
INSERT INTO dashboard_layouts (tenant_id, user_id, layout)
VALUES (@tenant_id, @user_id, @layout)
ON CONFLICT (tenant_id, user_id)
DO UPDATE SET layout = EXCLUDED.layout, updated_at = now();
