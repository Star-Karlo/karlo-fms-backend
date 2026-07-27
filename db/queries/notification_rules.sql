-- name: ListNotificationRules :many
SELECT * FROM notification_rules
WHERE (sqlc.narg('q')::text IS NULL OR rule_name ILIKE '%' || sqlc.narg('q') || '%' OR notification_name ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountNotificationRules :one
SELECT count(*) FROM notification_rules
WHERE (sqlc.narg('q')::text IS NULL OR rule_name ILIKE '%' || sqlc.narg('q') || '%' OR notification_name ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'));

-- name: GetNotificationRule :one
SELECT * FROM notification_rules WHERE id = $1;

-- name: CreateNotificationRule :one
INSERT INTO notification_rules (
  tenant_id, rule_name, alert_code, notification_name, urgency, status, applies_to,
  target, threshold, window_minutes, sent_to_self, geofence_scoped, time_scoped, auto_popup, delivery
) VALUES (
  $1, $2, $3, sqlc.narg('notification_name'), COALESCE(sqlc.narg('urgency'),'important'),
  COALESCE(sqlc.narg('status'),'active'), COALESCE(sqlc.narg('applies_to'),'all'),
  COALESCE(sqlc.narg('target')::jsonb, '{}'::jsonb), sqlc.narg('threshold'), sqlc.narg('window_minutes'),
  COALESCE(sqlc.narg('sent_to_self'), false), COALESCE(sqlc.narg('geofence_scoped'), false),
  COALESCE(sqlc.narg('time_scoped'), false), COALESCE(sqlc.narg('auto_popup'), false),
  COALESCE(sqlc.narg('delivery')::jsonb, '{}'::jsonb)
)
RETURNING *;

-- name: UpdateNotificationRule :one
UPDATE notification_rules
SET rule_name         = COALESCE(sqlc.narg('rule_name'), rule_name),
    notification_name = COALESCE(sqlc.narg('notification_name'), notification_name),
    urgency           = COALESCE(sqlc.narg('urgency'), urgency),
    status            = COALESCE(sqlc.narg('status'), status),
    applies_to        = COALESCE(sqlc.narg('applies_to'), applies_to),
    target            = COALESCE(sqlc.narg('target'), target),
    threshold         = COALESCE(sqlc.narg('threshold'), threshold),
    window_minutes    = COALESCE(sqlc.narg('window_minutes'), window_minutes),
    sent_to_self      = COALESCE(sqlc.narg('sent_to_self'), sent_to_self),
    geofence_scoped   = COALESCE(sqlc.narg('geofence_scoped'), geofence_scoped),
    time_scoped       = COALESCE(sqlc.narg('time_scoped'), time_scoped),
    auto_popup        = COALESCE(sqlc.narg('auto_popup'), auto_popup),
    delivery          = COALESCE(sqlc.narg('delivery'), delivery)
WHERE id = $1
RETURNING *;

-- name: DeleteNotificationRule :exec
DELETE FROM notification_rules WHERE id = $1;
