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

# blitz-action.sh against sample events, with a fake gh.

set -euo pipefail

script="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat > "$work/bin/gh" <<'GH'
#!/bin/sh
echo "gh $*" >> "$GH_LOG"
for a in "$@"; do case "$prev" in --body-file) cat "$a" >> "$GH_LOG" ;; esac; prev="$a"; done
GH
chmod +x "$work/bin/gh"
export PATH="$work/bin:$PATH" GH_LOG="$work/gh.log" RUNNER_TEMP="$work"
fail() { echo "FAIL: $*"; exit 1; }

event() { printf '%s' "$1" > "$work/event.json"; export GITHUB_EVENT_PATH="$work/event.json"; }
expect() { # mode, want (run=…), event
  event "$3"
  # sed reads to the end: head would exit after a line, and decide's next
  # write would then die of SIGPIPE, failing the test under pipefail.
  got="$(MODE="$1" bash "$script" decide | sed -n 1p)"
  [ "$got" = "$2" ] || fail "$1 on $3: $got, want $2"
}

mention='{"issue": {"number": 7, "title": "Crash on start", "body": "It crashes."}, "comment": {"body": "@blitz why?", "author_association": "MEMBER"}}'
expect auto run=true "$mention"
expect auto run=false '{"issue": {"number": 7}, "comment": {"body": "@blitz why?", "author_association": "NONE"}}'
expect auto run=false '{"issue": {"number": 7}, "comment": {"body": "thanks", "author_association": "OWNER"}}'
pr='{"pull_request": {"number": 9, "title": "Add cache", "body": "Adds a cache.", "base": {"ref": "main"}}}'
expect review run=true "$pr"
expect review run=false "$mention"
issue='{"issue": {"number": 12, "title": "Add --json", "body": "Please add --json."}}'
expect issue-to-pr run=true "$issue"
expect issue-to-pr run=false "$pr"
expect nonsense run=false "$pr"
TRIGGER=/ask expect auto run=true '{"issue": {"number": 1}, "comment": {"body": "/ask hi", "author_association": "OWNER"}}'

event "$mention"
p="$(MODE=auto bash "$script" prompt)"
case "$p" in *"On issue #7, \"Crash on start\", someone asked you:"*"@blitz why?"*"It crashes."*) ;; *) fail "auto prompt: $p" ;; esac
event "$pr"
p="$(MODE=review bash "$script" prompt)"
case "$p" in *"Review pull request #9"*"git diff origin/main...HEAD"*) ;; *) fail "review prompt: $p" ;; esac
p="$(MODE=review PROMPT="just this" bash "$script" prompt)"
[ "$p" = "just this" ] || fail "PROMPT: $p"

# A comment with the answer and the cost.
event "$mention"
echo '{"result": "Because X.", "is_error": false, "cost_usd": 0.1234}' > "$work/result.json"
MODE=auto bash "$script" post "$work/result.json"
grep -q "gh issue comment 7 --body-file" "$GH_LOG" || fail "no comment: $(cat "$GH_LOG")"
grep -q "Because X." "$GH_LOG" || fail "no answer in the comment"
grep -q 'Blitz, \$0.12' "$GH_LOG" || fail "no cost: $(cat "$GH_LOG")"
echo '{"result": "", "is_error": true, "error": "cost limit"}' > "$work/result.json"
MODE=auto bash "$script" post "$work/result.json"
grep -q "Blitz couldn't finish: cost limit" "$GH_LOG" || fail "no failure note"

# issue-to-pr: the changes go on a branch, pushed, with a pull request.
git init -q --bare "$work/remote.git"
git init -q "$work/repo"
cd "$work/repo"
git -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m first
git remote add origin "$work/remote.git"
echo change > file.txt
event "$issue"
echo '{"result": "Added --json.", "is_error": false}' > "$work/result.json"
: > "$GH_LOG"
MODE=issue-to-pr bash "$script" post "$work/result.json"
git --git-dir="$work/remote.git" rev-parse --verify -q refs/heads/blitz/issue-12 >/dev/null || fail "branch not pushed"
grep -q "gh pr create --head blitz/issue-12 --title Add --json" "$GH_LOG" || fail "no pull request: $(cat "$GH_LOG")"
grep -q "Resolves #12." "$GH_LOG" || fail "no Resolves line"
echo PASS
