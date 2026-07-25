#!/usr/bin/env bash
#
# Exercises a running notifier through its HTTP API.
#
#   Terminal 1:  make run
#   Terminal 2:  ./scripts/demo.sh
#
# Set BASE_URL to point at a different instance.

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"

bold() { printf '\n\033[1m%s\033[0m\n' "$*"; }
note() { printf '  \033[2m%s\033[0m\n' "$*"; }

# pretty pipes JSON through python3 when available, otherwise passes it through.
pretty() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -m json.tool
  else
    cat
  fi
}

# field extracts a top-level string field from a JSON object on stdin.
field() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "import json,sys; print(json.load(sys.stdin).get('$1',''))"
  else
    sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p"
  fi
}

if ! curl -sf "${BASE_URL}/healthz" >/dev/null; then
  echo "notifier is not reachable at ${BASE_URL} — start it with 'make run'" >&2
  exit 1
fi

bold "1. Channels this deployment supports"
note "The list comes from the provider registry, not a hard-coded constant."
curl -s "${BASE_URL}/v1/channels" | pretty

bold "2. Submit an email to three recipients"
note "202 Accepted: the request is queued, delivery has not happened yet."
response=$(curl -s -X POST "${BASE_URL}/v1/notifications" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: demo-$(date +%s)" \
  -d '{
        "channel": "email",
        "recipients": [
          {"id": "user-1", "address": "alice@example.com"},
          {"id": "user-2", "address": "bob@example.com"},
          {"id": "user-3", "address": "carol@example.com"}
        ],
        "message": {"subject": "Welcome", "body": "Thanks for signing up."},
        "metadata": {"campaign": "onboarding"}
      }')
echo "${response}" | pretty
notification_id=$(echo "${response}" | field id)

bold "3. Poll until the notification reaches a terminal state"
note "Each recipient is validated and delivered independently."
for _ in $(seq 1 40); do
  status_body=$(curl -s "${BASE_URL}/v1/notifications/${notification_id}")
  status=$(echo "${status_body}" | field status)
  case "${status}" in
    delivered|failed|partially_delivered) break ;;
  esac
  sleep 0.25
done
echo "${status_body}" | pretty

bold "4. Validation stops at the first failure"
note "One field is reported, with a code a client can branch on."
curl -s -X POST "${BASE_URL}/v1/notifications" \
  -H 'Content-Type: application/json' \
  -d '{"channel":"sms","recipients":[{"address":"+14155550123"},{"address":"not-a-phone"}],"message":{"body":"Hi"}}' \
  | pretty

bold "5. Unsupported channel"
curl -s -X POST "${BASE_URL}/v1/notifications" \
  -H 'Content-Type: application/json' \
  -d '{"channel":"carrier-pigeon","recipients":[{"address":"a@example.com"}],"message":{"body":"Hi"}}' \
  | pretty

bold "6. Push notification"
curl -s -X POST "${BASE_URL}/v1/notifications" \
  -H 'Content-Type: application/json' \
  -d '{"channel":"push","recipients":[{"id":"user-1","address":"dGhpcy1pcy1hLWRldmljZS10b2tlbg"}],"message":{"subject":"New message","body":"You have one unread message."}}' \
  | pretty

bold "7. Recent notifications"
curl -s "${BASE_URL}/v1/notifications?limit=5" | pretty

bold "8. Readiness, including queue depth and breaker states"
note "This is what an operator looks at during an incident."
curl -s "${BASE_URL}/readyz" | pretty

bold "Done."
