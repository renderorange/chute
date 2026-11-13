#!/usr/bin/env bash
# Vendored from renderorange/.github:.github/actions/slack-notify/run.sh
# Public repositories cannot use actions from private repositories, so chute
# carries its own copy. Keep it identical to the source and update there first.
set -euo pipefail

if [ -z "${SLACK_WEBHOOK_URL:-}" ]; then
  echo "::error::SLACK_WEBHOOK_URL is empty; caller job must set it from a secret"
  exit 1
fi

text=""
repo="${IN_REPO:-${GITHUB_REPOSITORY:-}}"
if [ -z "$repo" ]; then
  echo "::error::repo is empty; caller must pass repo or GITHUB_REPOSITORY must be set"
  exit 1
fi
branch="${IN_BRANCH:-${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-}}}"
sha_raw="${IN_SHA:-${GITHUB_SHA:-}}"
short_sha="${sha_raw:0:7}"
[ -n "$short_sha" ] || short_sha="unknown"
case "$IN_TEMPLATE" in
  pr-event)
    pr_url="https://github.com/${repo}/pull/${IN_PR_NUMBER}"
    head="[${repo}] PR #${IN_PR_NUMBER} ${IN_STATUS} · ${IN_PR_TITLE} by ${IN_AUTHOR}"
    text=$(jq -n \
      --arg h "$head" --arg b "$branch" --arg c "$short_sha" --arg u "$pr_url" \
      'if $b != "" and $c != "unknown" then [$h, ("branch: " + $b), ("commit: " + $c), $u] else [$h, $u] end | join("\n")')
    ;;
  review)
    run_url="https://github.com/${repo}/actions/runs/${IN_RUN_ID}"
    verdict="failed"
    [ "$IN_CONCLUSION" = "success" ] && verdict="succeeded"
    head="[${repo}] review ${verdict}"
    if [ "$IN_CONCLUSION" = "success" ] && [ -n "$IN_SUMMARY" ]; then
      head="${head} · ${IN_SUMMARY}"
    fi
    text=$(jq -n --arg h "$head" --arg c "$short_sha" --arg b "$branch" --arg u "$run_url" \
      'if $b != "" then [$h, ("branch: " + $b), ("commit: " + $c), $u] else [$h, ("commit: " + $c), $u] end | join("\n")')
    ;;
  job)
    run_url="https://github.com/${repo}/actions/runs/${IN_RUN_ID}"
    verdict=$(printf '%s' "$IN_CONCLUSION" | tr '[:upper:]' '[:lower:]')
    case "$IN_CONCLUSION" in
      success)   verdict="succeeded" ;;
      failure)   verdict="failed" ;;
      cancelled) verdict="cancelled" ;;
      timed_out) verdict="timed out" ;;
    esac
    head="[${repo}] ${IN_JOB} ${verdict}"
    [ -n "$IN_SUMMARY" ] && head="${head} · ${IN_SUMMARY}"
    text=$(jq -n --arg h "$head" --arg c "$short_sha" --arg b "$branch" --arg u "$run_url" \
      'if $b != "" then [$h, ("branch: " + $b), ("commit: " + $c), $u] else [$h, ("commit: " + $c), $u] end | join("\n")')
    ;;
  *)
    echo "::error::unknown template '${IN_TEMPLATE}'"
    exit 1
    ;;
esac

if [ "$IN_DRY_RUN" = "true" ]; then
  echo "DRY_RUN payload: $text"
  exit 0
fi

resp=$(curl -sS -o /tmp/slack_response.$$ -w '%{http_code}' -X POST \
  -H 'Content-type: application/json' \
  --data "{\"text\": $text}" "$SLACK_WEBHOOK_URL")
rm -f /tmp/slack_response.$$
if ! echo "$resp" | grep -q '^200$'; then
  echo "::error::slack webhook returned HTTP $resp"
  exit 1
fi
echo "slack notified: $IN_TEMPLATE"