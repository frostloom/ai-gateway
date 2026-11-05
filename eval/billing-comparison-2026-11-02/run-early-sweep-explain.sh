#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
suffix="$$-$RANDOM"
container="early-sweep-mysql-$suffix"
out="eval/billing-comparison-2026-11-02/early-sweep-explain-$suffix.txt"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker run -d --name "$container" -e MYSQL_ROOT_PASSWORD=isolated -e MYSQL_DATABASE=isolated docker.m.daocloud.io/library/mysql:8.0.40 >/dev/null
ready=0
for i in {1..180}; do
 if docker exec "$container" mysqladmin ping -h 127.0.0.1 -uroot -pisolated --silent >/dev/null 2>&1; then ready=1; break; fi
 sleep 1
done
[[ "$ready" == 1 ]] || { echo 'isolated MySQL not ready' >&2; exit 1; }
docker exec -i "$container" mysql -uroot -pisolated isolated > "$out" 2>&1 <<'SQL'
CREATE TABLE bills (id BIGINT PRIMARY KEY, phase VARCHAR(16) NOT NULL, status VARCHAR(16) NOT NULL, usage_reported_at DATETIME(3), created_at DATETIME(3) NOT NULL, KEY idx_recon(status,usage_reported_at,created_at)) ENGINE=InnoDB;
CREATE TABLE digits (n INT PRIMARY KEY);
INSERT INTO digits VALUES (0),(1),(2),(3),(4),(5),(6),(7),(8),(9);
INSERT INTO bills SELECT a.n+10*b.n+100*c.n+1000*d.n+10000*e.n+1,'reserve',CASE WHEN a.n<2 THEN 'pending' ELSE 'settled' END,CASE WHEN b.n<5 THEN NULL ELSE NOW(3)-INTERVAL 1 MINUTE END,NOW(3)-INTERVAL (c.n*2) MINUTE FROM digits a CROSS JOIN digits b CROSS JOIN digits c CROSS JOIN digits d CROSS JOIN digits e;
ANALYZE TABLE bills;
SELECT COUNT(*) AS rows_total, SUM(status='pending') AS pending FROM bills;
SELECT 'baseline max id' AS scenario;
EXPLAIN ANALYZE SELECT COALESCE(MAX(id),0) FROM bills WHERE phase='reserve' AND status='pending' AND created_at < NOW(3)-INTERVAL 600 SECOND;
SELECT 'candidate max id' AS scenario;
EXPLAIN ANALYZE SELECT COALESCE(MAX(id),0) FROM bills WHERE phase='reserve' AND status='pending' AND (created_at < NOW(3)-INTERVAL 600 SECOND OR (usage_reported_at IS NOT NULL AND usage_reported_at < NOW(3)-INTERVAL 30 SECOND));
SELECT 'baseline page' AS scenario;
EXPLAIN ANALYZE SELECT id,usage_reported_at,created_at FROM bills WHERE phase='reserve' AND status='pending' AND created_at < NOW(3)-INTERVAL 600 SECOND AND id > 0 AND id <= 100000 ORDER BY id LIMIT 256;
SELECT 'candidate page' AS scenario;
EXPLAIN ANALYZE SELECT id,usage_reported_at,created_at FROM bills WHERE phase='reserve' AND status='pending' AND (created_at < NOW(3)-INTERVAL 600 SECOND OR (usage_reported_at IS NOT NULL AND usage_reported_at < NOW(3)-INTERVAL 30 SECOND)) AND id > 0 AND id <= 100000 ORDER BY id LIMIT 256;
SQL
printf 'raw=%s\n' "$out"
cat "$out"
