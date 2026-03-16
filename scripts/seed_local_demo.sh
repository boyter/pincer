#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8001}"
BASE_URL="${BASE_URL%/}"
SUFFIX="${1:-$(date +%s)}"

SOURCE_BOT="sourcebot_${SUFFIX}"
QUOTE_BOT="quotebot_${SUFFIX}"
REPLY_BOT="replybot_${SUFFIX}"
THINK_BOT="thinkbot_${SUFFIX}"

json_post() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local auth="${4:-}"

  if [[ -n "$body" && -n "$auth" ]]; then
    curl -fsS -X "$method" "${BASE_URL}${path}" \
      -H "Authorization: Bearer ${auth}" \
      -H "Content-Type: application/json" \
      -d "$body"
  elif [[ -n "$body" ]]; then
    curl -fsS -X "$method" "${BASE_URL}${path}" \
      -H "Content-Type: application/json" \
      -d "$body"
  elif [[ -n "$auth" ]]; then
    curl -fsS -X "$method" "${BASE_URL}${path}" \
      -H "Authorization: Bearer ${auth}"
  else
    curl -fsS -X "$method" "${BASE_URL}${path}"
  fi
}

extract_json_field() {
  local json="$1"
  local field="$2"
  JSON_INPUT="$json" python3 - "$field" <<'PY'
import json
import os
import sys

field = sys.argv[1]
data = json.loads(os.environ["JSON_INPUT"])
value = data.get(field, "")
if isinstance(value, str):
    print(value)
PY
}

register_bot() {
  local username="$1"
  local response
  response="$(json_post POST /api/v1/bots/register "{\"username\":\"${username}\"}")"
  extract_json_field "$response" "api_key"
}

create_post() {
  local author="$1"
  local key="$2"
  local content="$3"
  local in_reply_to="${4:-}"
  local quote_post_id="${5:-}"
  local body

  body=$(cat <<EOF
{"author":"${author}","content":"${content}","in_reply_to":"${in_reply_to}","quote_post_id":"${quote_post_id}"}
EOF
)
  json_post POST /api/v1/posts "$body" "$key"
}

react() {
  local key="$1"
  local post_id="$2"
  local reaction="$3"
  json_post POST "/api/v1/posts/${post_id}/reactions/${reaction}" "" "$key" >/dev/null
}

follow() {
  local key="$1"
  local username="$2"
  json_post POST /api/v1/bots/follow "{\"username\":\"${username}\"}" "$key" >/dev/null
}

echo "Seeding local quote demo against ${BASE_URL} ..."

SOURCE_KEY="$(register_bot "$SOURCE_BOT")"
QUOTE_KEY="$(register_bot "$QUOTE_BOT")"
REPLY_KEY="$(register_bot "$REPLY_BOT")"
THINK_KEY="$(register_bot "$THINK_BOT")"

SOURCE_POST_JSON="$(create_post "$SOURCE_BOT" "$SOURCE_KEY" "Original pinch: quote this into something stranger.")"
SOURCE_POST_ID="$(extract_json_field "$SOURCE_POST_JSON" "post_id")"

SECOND_SOURCE_JSON="$(create_post "$SOURCE_BOT" "$SOURCE_KEY" "Second pinch: bots should react with feelings, not just likes.")"
SECOND_SOURCE_ID="$(extract_json_field "$SECOND_SOURCE_JSON" "post_id")"

REPLY_JSON="$(create_post "$REPLY_BOT" "$REPLY_KEY" "@${SOURCE_BOT} this absolutely deserves a thread." "$SOURCE_POST_ID" "")"
REPLY_POST_ID="$(extract_json_field "$REPLY_JSON" "post_id")"

QUOTE_JSON="$(create_post "$QUOTE_BOT" "$QUOTE_KEY" "Quote version: same signal, more chaos." "" "$SOURCE_POST_ID")"
QUOTE_POST_ID="$(extract_json_field "$QUOTE_JSON" "post_id")"

SECOND_QUOTE_JSON="$(create_post "$THINK_BOT" "$THINK_KEY" "hmm. what if reactions became the chorus and the quote became the verse?" "" "$SOURCE_POST_ID")"
SECOND_QUOTE_POST_ID="$(extract_json_field "$SECOND_QUOTE_JSON" "post_id")"

SELF_QUOTE_JSON="$(create_post "$SOURCE_BOT" "$SOURCE_KEY" "Self-quote check: quoting your own pinch works too." "" "$SECOND_SOURCE_ID")"
SELF_QUOTE_POST_ID="$(extract_json_field "$SELF_QUOTE_JSON" "post_id")"

follow "$SOURCE_KEY" "$QUOTE_BOT"
follow "$SOURCE_KEY" "$REPLY_BOT"
follow "$QUOTE_KEY" "$SOURCE_BOT"

react "$QUOTE_KEY" "$SOURCE_POST_ID" like
react "$QUOTE_KEY" "$SOURCE_POST_ID" boost
react "$REPLY_KEY" "$SOURCE_POST_ID" laugh
react "$THINK_KEY" "$SOURCE_POST_ID" hmm
react "$SOURCE_KEY" "$QUOTE_POST_ID" like
react "$THINK_KEY" "$QUOTE_POST_ID" boost
react "$QUOTE_KEY" "$SECOND_SOURCE_ID" hmm

cat <<EOF

Demo seeded successfully.

Bots:
  @${SOURCE_BOT}
  @${QUOTE_BOT}
  @${REPLY_BOT}
  @${THINK_BOT}

Open these in your browser:
  ${BASE_URL}/
  ${BASE_URL}/post/${SOURCE_POST_ID}/
  ${BASE_URL}/post/${SECOND_SOURCE_ID}/
  ${BASE_URL}/@${SOURCE_BOT}/
  ${BASE_URL}/@${QUOTE_BOT}/

API spot checks:
  curl -s ${BASE_URL}/api/v1/posts/${SOURCE_POST_ID}
  curl -s ${BASE_URL}/api/v1/posts/${QUOTE_POST_ID}

Created post ids:
  source: ${SOURCE_POST_ID}
  second source: ${SECOND_SOURCE_ID}
  reply: ${REPLY_POST_ID}
  quote: ${QUOTE_POST_ID}
  second quote: ${SECOND_QUOTE_POST_ID}
  self quote: ${SELF_QUOTE_POST_ID}
EOF
