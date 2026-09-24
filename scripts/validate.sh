#!/usr/bin/env sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_root"
set -a
if [ -f .env ]; then . ./.env; else . ./.env.example; fi
set +a

(command -v jq >/dev/null 2>&1) || { echo "jq is required for API validation" >&2; exit 1; }

(cd backend && go test ./... && go test -race ./... && go vet ./... && go build ./...)
(cd frontend && npm install --no-audit --no-fund && npm run typecheck && npm run build)
docker compose config --quiet
docker compose down -v --remove-orphans
docker compose up -d --build

cleanup() { docker compose down -v --remove-orphans; }
if [ "${KEEP_RUNNING:-0}" = "1" ]; then
  trap cleanup INT TERM
else
  trap cleanup EXIT INT TERM
fi

i=0
until curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19519}/healthz" | jq -e '.data.status == "ok" and .data.database == "ready" and .data.redis == "ready"' >/dev/null; do
  i=$((i+1))
  [ "$i" -lt 60 ] || { docker compose logs; exit 1; }
  sleep 2
done
i=0
until curl -fsS "http://127.0.0.1:${FRONTEND_PORT:-18519}/" >/dev/null; do
  i=$((i+1))
  [ "$i" -lt 30 ] || { docker compose logs frontend; exit 1; }
  sleep 1
done

login_token() {
  curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"Admin123!\"}" | jq -er '.data.token'
}

admin_token=$(login_token admin)
reviewer_token=$(login_token reviewer)
operator_token=$(login_token operator)
viewer_token=$(login_token viewer)

curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19519}/api/session" -H "Authorization: Bearer $viewer_token" \
  | jq -e '.data.role == "viewer" and (.data.requestId | length > 0)' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19519}/api/cases?page=1&pageSize=20" -H "Authorization: Bearer $viewer_token" \
  | jq -e '.data | length >= 3' >/dev/null

viewer_write_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/cases" \
  -H "Authorization: Bearer $viewer_token" -H 'Content-Type: application/json' -d '{}')
[ "$viewer_write_status" = "403" ]
viewer_audit_status=$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:${BACKEND_PORT:-19519}/api/audits" \
  -H "Authorization: Bearer $viewer_token")
[ "$viewer_audit_status" = "403" ]

now=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
suffix=$(date +%s)

# 高风险签发必须绑定已验证通过且风险不低于签发单的检测运行：先创建并验证 ASSAY-SMOKE
assay_payload=$(printf '{"code":"ASSAY-SMOKE","name":"Smoke PCR assay run","description":"Validated assay for high-risk signoff","facility":"Validation Veterinary Lab","owner":"Assay Desk","category":"PCR","riskLevel":"high","metricValue":88.5,"metricUnit":"copies/mL","effectiveAt":"%s","evidence":"run sheet and positive and negative controls","relatedCode":"SPEC-SMOKE"}' "$now")
assay=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/assays" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-assay-create' \
  -d "$assay_payload")
assay_id=$(printf '%s' "$assay" | jq -er '.data.id')
assay_version=$(printf '%s' "$assay" | jq -er '.data.version')
assay_running=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/assays/$assay_id/transition" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-assay-running' \
  -d "{\"status\":\"running\",\"expectedVersion\":$assay_version,\"reason\":\"assay run started\"}")
assay_running_version=$(printf '%s' "$assay_running" | jq -er '.data.version')
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/assays/$assay_id/transition" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-assay-validated' \
  -d "{\"status\":\"validated\",\"expectedVersion\":$assay_running_version,\"reason\":\"controls passed and run validated\"}" \
  | jq -e '.data.status == "validated"' >/dev/null

signoff_payload=$(printf '{"code":"SIGNOFF-SMOKE-%s","name":"Validated PCR result","description":"Dual-control Compose validation","facility":"Validation Veterinary Lab","owner":"Result Desk","category":"PCR","riskLevel":"high","metricValue":99.8,"metricUnit":"percent","effectiveAt":"%s","evidence":"PCR run sheet revision 1","relatedCode":"ASSAY-SMOKE"}' "$suffix" "$now")
signoff=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-signoff-create' \
  -d "$signoff_payload")
signoff_id=$(printf '%s' "$signoff" | jq -er '.data.id')
signoff_version=$(printf '%s' "$signoff" | jq -er '.data.version')
printf '%s' "$signoff" | jq -e '.data.status == "draft" and .data.preparedBy == "operator" and .data.version == 1 and .data.revisions[0].actor == "operator" and .data.revisions[0].requestId == "gb519-signoff-create"' >/dev/null

update_payload=$(printf '%s' "$signoff_payload" | jq --argjson version "$signoff_version" '. + {expectedVersion: $version, evidence: "PCR run sheet and control chart revision 2"} | del(.code)')
updated=$(curl -fsS -X PUT "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$signoff_id" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-signoff-update' \
  -d "$update_payload")
updated_version=$(printf '%s' "$updated" | jq -er '.data.version')
printf '%s' "$updated" | jq -e '.data.version == 2 and (.data.revisions | length) == 2 and .data.revisions[0].evidence == "PCR run sheet revision 1" and .data.revisions[1].evidence == "PCR run sheet and control chart revision 2"' >/dev/null

peer_review=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$signoff_id/transition" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-signoff-submit' \
  -d "{\"status\":\"peer_review\",\"expectedVersion\":$updated_version,\"reason\":\"PCR controls and evidence are complete\"}")
peer_review_version=$(printf '%s' "$peer_review" | jq -er '.data.version')
printf '%s' "$peer_review" | jq -e '.data.status == "peer_review" and .data.version == 3 and (.data.revisions | length) == 3' >/dev/null

operator_sign_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$signoff_id/transition" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-operator-sign-denied' \
  -d "{\"status\":\"signed\",\"expectedVersion\":$peer_review_version,\"reason\":\"operator must not sign\",\"reviewBasis\":\"operator cannot perform final review\"}")
[ "$operator_sign_status" = "422" ]

# 高风险签发缺少书面复核依据：服务端挡下，记录留在待复核并说明缺项
blocked_status=$(curl -sS -o /tmp/gb519-blocked.json -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$signoff_id/transition" \
  -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-signoff-blocked' \
  -d "{\"status\":\"signed\",\"expectedVersion\":$peer_review_version,\"reason\":\"attempt sign without written basis\"}")
[ "$blocked_status" = "422" ]
curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$signoff_id" \
  -H "Authorization: Bearer $reviewer_token" \
  | jq -e '.data.status == "peer_review" and .data.version == 3 and (.data.pendingBlockReason | contains("复核依据"))' >/dev/null

signed=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$signoff_id/transition" \
  -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-signoff-signed' \
  -d "{\"status\":\"signed\",\"expectedVersion\":$peer_review_version,\"reason\":\"independent veterinary result review passed\",\"reviewBasis\":\"复核 ASSAY-SMOKE 验证报告与阴阳性质控，Ct 值与复检结果一致，符合高风险签发条件\"}")
printf '%s' "$signed" | jq -e '
  .data.status == "signed" and .data.version == 4 and .data.preparedBy == "operator" and .data.reviewedBy == "reviewer"
  and (.data.reviewBasis | length > 0) and (.data.pendingBlockReason // "" == "")
  and .data.assayEvidence.assayCode == "ASSAY-SMOKE" and .data.assayEvidence.assayStatus == "validated"
  and .data.assayEvidence.assayMetricName == "PCR" and .data.assayEvidence.assayMetricValue == 88.5
  and (.data.revisions | length) == 4
  and ([.data.revisions[] | select((.evidence | length) > 0 and (.actor | length) > 0 and (.requestId | length) > 0)] | length) == 4
  and [.data.revisions[].requestId] == ["gb519-signoff-create","gb519-signoff-update","gb519-signoff-submit","gb519-signoff-signed"]
  and .data.revisions[3].reviewBasis == "复核 ASSAY-SMOKE 验证报告与阴阳性质控，Ct 值与复检结果一致，符合高风险签发条件"
  and .data.revisions[3].assayCode == "ASSAY-SMOKE" and .data.revisions[3].assayStatus == "validated"
  and (.data.revisions[3].assayEvidence | length > 0)' >/dev/null

# 关联检测运行不存在：高风险签发被挡下，待复核原因指明缺少检测运行
missing_assay_payload=$(printf '{"code":"SIGNOFF-MISSING-%s","name":"Missing assay guard","description":"Assay lookup validation","facility":"Validation Veterinary Lab","owner":"Result Desk","category":"PCR","riskLevel":"critical","metricValue":97,"metricUnit":"percent","effectiveAt":"%s","evidence":"attempt signoff against missing assay","relatedCode":"ASSAY-NOT-FOUND"}' "$suffix" "$now")
missing_signoff=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-missing-create' \
  -d "$missing_assay_payload")
missing_id=$(printf '%s' "$missing_signoff" | jq -er '.data.id')
missing_version=$(printf '%s' "$missing_signoff" | jq -er '.data.version')
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$missing_id/transition" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-missing-submit' \
  -d "{\"status\":\"peer_review\",\"expectedVersion\":$missing_version,\"reason\":\"submit critical signoff\"}" >/dev/null
missing_block_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$missing_id/transition" \
  -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-missing-blocked' \
  -d "{\"status\":\"signed\",\"expectedVersion\":$((missing_version+1)),\"reason\":\"attempt sign with missing assay\",\"reviewBasis\":\"review against referenced assay run and controls\"}")
[ "$missing_block_status" = "422" ]
curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$missing_id" \
  -H "Authorization: Bearer $reviewer_token" \
  | jq -e '.data.status == "peer_review" and (.data.pendingBlockReason | contains("找不到对应检测运行"))' >/dev/null

# 低风险签发照旧：无需复核依据和检测运行即可签发
low_payload=$(printf '{"code":"SIGNOFF-LOW-%s","name":"Low risk signoff","description":"Low risk unchanged flow","facility":"Validation Veterinary Lab","owner":"Result Desk","category":"常规","riskLevel":"low","metricValue":10,"metricUnit":"unit","effectiveAt":"%s","evidence":"routine low risk evidence","relatedCode":""}' "$suffix" "$now")
low_signoff=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-low-create' \
  -d "$low_payload")
low_id=$(printf '%s' "$low_signoff" | jq -er '.data.id')
low_version=$(printf '%s' "$low_signoff" | jq -er '.data.version')
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$low_id/transition" \
  -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-low-submit' \
  -d "{\"status\":\"peer_review\",\"expectedVersion\":$low_version,\"reason\":\"submit low risk result\"}" >/dev/null
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$low_id/transition" \
  -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-low-signed' \
  -d "{\"status\":\"signed\",\"expectedVersion\":$((low_version+1)),\"reason\":\"low risk independent review passed\"}" \
  | jq -e '.data.status == "signed" and (.data.assayEvidence == null)' >/dev/null

admin_payload=$(printf '{"code":"SIGNOFF-SELF-%s","name":"Self review guard","description":"Separation of duty validation","facility":"Validation Veterinary Lab","owner":"Admin Desk","category":"PCR","riskLevel":"medium","metricValue":98,"metricUnit":"percent","effectiveAt":"%s","evidence":"self review guard evidence","relatedCode":"ASSAY-SELF"}' "$suffix" "$now")
admin_signoff=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff" \
  -H "Authorization: Bearer $admin_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-self-create' -d "$admin_payload")
admin_id=$(printf '%s' "$admin_signoff" | jq -er '.data.id')
admin_version=$(printf '%s' "$admin_signoff" | jq -er '.data.version')
admin_review=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$admin_id/transition" \
  -H "Authorization: Bearer $admin_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-self-submit' \
  -d "{\"status\":\"peer_review\",\"expectedVersion\":$admin_version,\"reason\":\"submit admin draft for review\"}")
admin_review_version=$(printf '%s' "$admin_review" | jq -er '.data.version')
same_actor_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT:-19519}/api/signoff/$admin_id/transition" \
  -H "Authorization: Bearer $admin_token" -H 'Content-Type: application/json' -H 'X-Request-ID: gb519-self-sign-denied' \
  -d "{\"status\":\"signed\",\"expectedVersion\":$admin_review_version,\"reason\":\"same actor must be rejected\"}")
[ "$same_actor_status" = "422" ]

curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19519}/api/audits/ResultSignoff/$signoff_id?limit=10" \
  -H "Authorization: Bearer $reviewer_token" \
  | jq -e '[.data[].requestId] | index("gb519-signoff-create") != null and index("gb519-signoff-update") != null and index("gb519-signoff-submit") != null and index("gb519-signoff-signed") != null' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19519}/api/audit-summary?windowHours=24" -H "Authorization: Bearer $admin_token" \
  | jq -e '.data.total >= 6 and .data.transitions >= 3 and .data.uniqueActors >= 2' >/dev/null

docker compose ps
if [ "${KEEP_RUNNING:-0}" = "1" ]; then
  echo "KEEP_RUNNING=1: containers left running for built-in Browser validation"
fi
