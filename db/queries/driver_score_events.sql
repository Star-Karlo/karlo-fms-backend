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
-- metadata carries the numbers behind each penalty (idle %, the specific dates
-- an issue occurred); the newest row's is kept, though the collector writes one
-- row per behaviour so there is only ever one.
SELECT event_type,
       COALESCE(sum(occurrences),0)::bigint AS occurrences,
       COALESCE(sum(score),0)::numeric AS score,
       (jsonb_agg(metadata ORDER BY created_at DESC) -> 0) AS metadata
FROM driver_score_events
WHERE driver_id = $1 AND period = sqlc.arg('period')
GROUP BY event_type;
