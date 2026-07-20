-- name: AttachRuleVehicle :exec
INSERT INTO rule_vehicles (tenant_id, rule_id, vehicle_id)
VALUES (@tenant_id, @rule_id, @vehicle_id)
ON CONFLICT (tenant_id, rule_id, vehicle_id) DO NOTHING;

-- name: DetachRuleVehicle :exec
DELETE FROM rule_vehicles WHERE rule_id = @rule_id AND vehicle_id = @vehicle_id;

-- name: ListRuleVehicles :many
-- Vehicles attached to a rule (with plate for display).
SELECT rv.vehicle_id, v.license_plate
FROM rule_vehicles rv
JOIN vehicles v ON v.id = rv.vehicle_id
WHERE rv.rule_id = @rule_id
ORDER BY v.license_plate;

-- name: ListRuleIDsForVehicle :many
-- Rule ids attached to a vehicle (to render toggles on the vehicle panel).
SELECT rule_id FROM rule_vehicles WHERE vehicle_id = @vehicle_id;

-- name: ListActiveRuleTargets :many
-- Every (active rule x attached vehicle) with the tracker IMEI, for the engine.
SELECT r.id AS rule_id, r.alert_code, r.threshold, r.urgency, r.rule_name,
       r.notification_name, rv.vehicle_id, t.imei
FROM notification_rules r
JOIN rule_vehicles rv ON rv.rule_id = r.id
JOIN vehicles v       ON v.id = rv.vehicle_id
LEFT JOIN trackers t  ON t.id = v.tracker_id
WHERE r.status = 'active';
