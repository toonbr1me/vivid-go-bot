-- Каталог тарифов и пакетов устройств. Эти запросы повторяют ручные
-- из internal/storage/plans.go: когда sqlc станет частью сборки, они
-- заменят ручной Scan, а имена колонок останутся те же.

-- name: ListPlans :many
SELECT plan_id, title, days, price_rub, devices, sort_order, is_active
FROM plans
ORDER BY sort_order, plan_id;

-- name: GetPlan :one
SELECT plan_id, title, days, price_rub, devices, sort_order, is_active
FROM plans
WHERE plan_id = $1;

-- name: ListDevicePacks :many
SELECT pack_id, title, devices, price_rub, days, sort_order, is_active
FROM device_packs
ORDER BY sort_order, pack_id;

-- name: GetDevicePack :one
SELECT pack_id, title, devices, price_rub, days, sort_order, is_active
FROM device_packs
WHERE pack_id = $1;
