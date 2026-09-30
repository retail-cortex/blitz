#!/usr/bin/env bash
# Copyright 2026 Retail Cortex
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# The GitHub Action's steps (spec_parity_027 PAR-INT-01), from the event in
# GITHUB_EVENT_PATH:
#
#   blitz-action.sh decide   prints run=true|false and why, for GITHUB_OUTPUT
#   blitz-action.sh prompt   prints the prompt for blitz exec
#   blitz-action.sh post <result.json>   comments the answer, or opens a
#                                         pull request with the changes
#
# MODE is auto (answer a mention), review (review the pull request) or
# issue-to-pr (implement the issue on a branch); TRIGGER is the phrase a
# comment must hold (auto mode); PROMPT, when set, replaces the made-up one.

set -euo pipefail

event="${GITHUB_EVENT_PATH:?no GITHUB_EVENT_PATH}"
mode="${MODE:-auto}"
trigger="${TRIGGER:-@blitz}"

j() { jq -r "$1 // empty" "$event"; }

# number is the issue or pull request the event is about.
number() { j '.pull_request.number // .issue.number'; }

is_pr() { [ -n "$(j '.pull_request.number // .issue.pull_request.url')" ]; }

decide() {
  case "$mode" in
    review)
      if is_pr; then echo "run=true"; else echo "run=false"; echo "reason=review needs a pull request event"; fi
      return ;;
    issue-to-pr)
      if [ -n "$(j '.issue.number')" ] && ! is_pr; then echo "run=true"; else echo "run=false"; echo "reason=issue-to-pr needs an issue event"; fi
      return ;;
    auto) ;;
    *) echo "run=false"; echo "reason=unknown mode $mode"; return ;;
  esac
  body="$(j '.comment.body // .review.body // .issue.body')"
  case "$body" in
    *"$trigger"*) ;;
    *) echo "run=false"; echo "reason=no $trigger in the text"; return ;;
  esac
  # Only people with write access to the repository can start a run.
  who="$(j '.comment.author_association // .review.author_association // .issue.author_association')"
  case "$who" in
    OWNER | MEMBER | COLLABORATOR) echo "run=true" ;;
    *) echo "run=false"; echo "reason=$who can't start a run: only owners, members and collaborators" ;;
  esac
}

prompt() {
  if [ -n "${PROMPT:-}" ]; then
    printf '%s\n' "$PROMPT"
    return
  fi
  n="$(number)"
  title="$(j '.pull_request.title // .issue.title')"
  body="$(j '.pull_request.body // .issue.body')"
  case "$mode" in
    review)
      base="$(j '.pull_request.base.ref')"
      printf 'Review pull request #%s, "%s", against origin/%s: run git diff origin/%s...HEAD and read what it touches. Report bugs, security problems and missing tests, most serious first, each with its file and line; say plainly if you find nothing. Change nothing.\n\nIts description:\n%s\n' \
        "$n" "$title" "${base:-main}" "${base:-main}" "$body"
      ;;
    issue-to-pr)
      printf 'Resolve issue #%s, "%s". Make the change in this repository, with tests, and check it builds and the tests pass. Don'"'"'t commit or push: that is done for you. Finish with a short summary of what you changed, for the pull request.\n\nThe issue:\n%s\n' "$n" "$title" "$body"
      ;;
    *)
      ask="$(j '.comment.body // .review.body // .issue.body')"
      kind=issue
      is_pr && kind="pull request"
      printf 'On %s #%s, "%s", someone asked you:\n\n%s\n\nAnswer in a comment (GitHub Markdown). Change nothing unless asked to.\n\nThe %s'"'"'s description:\n%s\n' \
        "$kind" "$n" "$title" "$ask" "$kind" "$body"
      ;;
  esac
}

post() {
  result="${1:?post needs the result file}"
  n="$(number)"
  answer="$(jq -r '.result // ""' "$result")"
  failed="$(jq -r '.is_error // false' "$result")"
  cost="$(jq -r 'if .cost_usd then "$" + (.cost_usd * 100 | round / 100 | tostring) else "" end' "$result")"
  footer="—
*Blitz${cost:+, $cost}*"
  if [ "$failed" = true ]; then
    answer="Blitz couldn't finish: $(jq -r '.error // "unknown error"' "$result")

$answer"
  fi
  if [ "$mode" = issue-to-pr ] && [ "$failed" != true ] && [ -n "$(git status --porcelain)" ]; then
    branch="blitz/issue-$n"
    git checkout -b "$branch"
    git add -A
    git -c user.name="blitz[bot]" -c user.email="blitz-bot@users.noreply.github.com" commit -q -m "Resolve #$n" -m "$answer"
    git push -q origin "$branch"
    gh pr create --head "$branch" --title "$(j '.issue.title')" --body "$answer

Resolves #$n.
$footer"
    return
  fi
  printf '%s\n\n%s\n' "$answer" "$footer" > "${RUNNER_TEMP:-/tmp}/blitz-comment.md"
  gh issue comment "$n" --body-file "${RUNNER_TEMP:-/tmp}/blitz-comment.md"
}

case "${1:-}" in
  decide) decide ;;
  prompt) prompt ;;
  post) post "${2:-}" ;;
  *) echo "usage: blitz-action.sh decide|prompt|post <result.json>" >&2; exit 2 ;;
esac
