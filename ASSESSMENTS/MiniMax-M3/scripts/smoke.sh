#!/usr/bin/env bash
set -euo pipefail
BASE=${BASE:-http://127.0.0.1:8080}
echo "== healthz =="
curl -fsS "$BASE/healthz"; echo
echo "== submit =="
SUBMIT=$(curl -fsS -X POST "$BASE/v1/notifications" \
  -H 'content-type: application/json' \
  -d '{"recipients":["alice@example.com","bob@example.com"],"channel":"email","message":{"subject":"hi","body":"hello"}}')
echo "$SUBMIT"
ID=$(echo "$SUBMIT" | python3 -c 'import sys, json; print(json.load(sys.stdin)["notifications"][0]["id"])')
echo "id=$ID"
echo "== wait for terminal status =="
for i in $(seq 1 50); do
  RESP=$(curl -fsS "$BASE/v1/notifications/$ID")
  STATUS=$(echo "$RESP" | python3 -c 'import sys, json; print(json.load(sys.stdin)["status"])')
  echo "  attempt $i: $STATUS"
  case "$STATUS" in
    delivered|failed|rejected) break ;;
  esac
  sleep 0.1
done
echo "== list =="
curl -fsS "$BASE/v1/notifications?limit=5" | python3 -m json.tool | head -40
echo "== list filtered =="
curl -fsS "$BASE/v1/notifications?channel=email&limit=2" | python3 -m json.tool | head -40
echo "== invalid channel =="
curl -s -o /dev/null -w "status=%{http_code}\n" -X POST "$BASE/v1/notifications" \
  -H 'content-type: application/json' \
  -d '{"recipients":["a@b.com"],"channel":"fax","message":{"body":"x"}}'
echo "== unknown id =="
curl -s -o /dev/null -w "status=%{http_code}\n" "$BASE/v1/notifications/does-not-exist"
echo "done"
