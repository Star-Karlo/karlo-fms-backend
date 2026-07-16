-- name: DriverScoreSummary :many
-- Per-driver aggregate for a month (period is the first day of the month).
SELECT
  e.id            AS driver_id,
  e.full_name     AS driver_name,
  e.status        AS driver_status,
  COALESCE(sum(ev.occurrences), 0)::bigint AS total_occurrences,
  COALESCE(sum(ev.score), 0)::numeric      AS total_score,
  COALESCE((SELECT sum(sc.score) FROM driver_score_events sc WHERE sc.driver_id = e.id AND sc.period = sqlc.arg('period') AND sc.event_type = 'trip_km'), 0)::numeric AS trip_km
FROM employees e
JOIN driver_score_events ev ON ev.driver_id = e.id AND ev.period = sqlc.arg('period') AND ev.event_type <> 'trip_km'
WHERE e.tenant_id = e.tenant_id
GROUP BY e.id, e.full_name, e.status
ORDER BY total_score DESC;

-- name: DriverScoreDetail :many
-- Per-behavior breakdown for one driver in a month.
SELECT event_type, COALESCE(sum(occurrences),0)::bigint AS occurrences, COALESCE(sum(score),0)::numeric AS score
FROM driver_score_events
WHERE driver_id = $1 AND period = sqlc.arg('period')
GROUP BY event_type;

-- name: DriverScoreEvents :many
-- Raw event log for a driver (the Alert List on the detail page).
SELECT ev.event_type, ev.occurrences, ev.score, ev.created_at, v.license_plate
FROM driver_score_events ev
LEFT JOIN vehicles v ON v.id = ev.vehicle_id
WHERE ev.driver_id = $1 AND ev.period = sqlc.arg('period') AND ev.event_type <> 'trip_km'
ORDER BY ev.created_at DESC
LIMIT sqlc.arg('lim');
