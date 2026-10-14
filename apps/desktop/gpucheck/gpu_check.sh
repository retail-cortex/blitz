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

# How the Linux desktop app draws (DSK-100), on a machine with a display
# and a graphics card; a manual check, not part of bazel test.
#
#   bazel run //apps/desktop/gpucheck:gpu_check [-- --seconds 60 --max-cpu 5 on off]
#
# For each mode (the gpu preference, "on" and "off" by default):
#
#  1. The app: a throwaway HOME with a keyless blitzd and an open, empty
#     workspace, the window on a private D-Bus session (so a running Blitz
#     doesn't take it over).
#  2. WebKit's own report (webkit://gpu, gpu_probe.c) with the app's
#     policy and the WEBKIT_*, __NV_* and GDK_BACKEND variables its web process
#     started with (what linuxOptions set): "on" must draw with the
#     graphics card (a policy other than never, GPU painting), "off" in
#     software.
#  3. The app, idle: after a warm-up, the CPU its processes (the window,
#     WebKit's) use over --seconds must stay under --max-cpu percent of
#     one core, and it must still be running.
#
# Leave the window uncovered while it runs. Exits 1 when a check fails.
set -euo pipefail

seconds=60
warmup=10
max_cpu=5
modes=()
while [ $# -gt 0 ]; do
	case "$1" in
	--seconds) seconds="$2"; shift 2 ;;
	--warmup) warmup="$2"; shift 2 ;;
	--max-cpu) max_cpu="$2"; shift 2 ;;
	on | off) modes+=("$1"); shift ;;
	*) echo "usage: gpu_check [--seconds N] [--warmup N] [--max-cpu PCT] [on] [off]" >&2; exit 2 ;;
	esac
done
[ ${#modes[@]} -gt 0 ] || modes=(on off)

if [ "$(uname -s)" != Linux ]; then
	echo "gpu_check: Linux only (the GPU setting is the Linux app's)" >&2
	exit 2
fi
if [ -z "${WAYLAND_DISPLAY:-}" ] && [ -z "${DISPLAY:-}" ]; then
	echo "gpu_check: needs a display" >&2
	exit 2
fi

runfiles="${RUNFILES_DIR:-$0.runfiles}/_main"
probe="$runfiles/apps/desktop/gpucheck/gpu_probe_/gpu_probe"
app="$runfiles/apps/desktop/blitz-desktop_unsigned_/blitz-desktop_unsigned"
blitzd="$runfiles/apps/service/blitzd"
for f in "$probe" "$app" "$blitzd"; do
	[ -x "$f" ] || { echo "gpu_check: missing $f (run with bazel run)" >&2; exit 2; }
done
hz="$(getconf CLK_TCK)"

# field <report> <label>: the line after <label> in WebKit's report.
field() { awk -v k="$2" 'found { print; exit } $0 == k { found = 1 }' <<<"$1"; }

# ticks <sid>: "pid name ticks" for each process in the session, its CPU
# time so far (user and system, in clock ticks).
ticks() {
	local pid stat
	for pid in $(pgrep -s "$1"); do
		stat="$(cat "/proc/$pid/stat" 2>/dev/null)" || continue
		# The name is in parentheses and may hold spaces: fields after it.
		awk -v pid="$pid" -v name="$(cat "/proc/$pid/comm" 2>/dev/null)" \
			'{ print pid, name, $12 + $13 }' <<<"${stat##*) }"
	done
}

failed=0
fail() { echo "  ✗ $*"; failed=1; }
pass() { echo "  ✓ $*"; }

tmp=""
session=""
service=""
cleanup() {
	[ -n "$session" ] && kill -- "-$session" 2>/dev/null || true
	[ -n "$service" ] && kill "$service" 2>/dev/null || true
	wait 2>/dev/null || true
	[ -n "$tmp" ] && rm -rf "$tmp"
	session="" service="" tmp=""
}
trap cleanup EXIT

for mode in "${modes[@]}"; do
	echo "== gpu $mode"
	# 1. The app.
	tmp="$(mktemp -d /tmp/blitz-gpu.XXXXXX)" # short: the socket's path
	mkdir "$tmp/ws"
	echo "A workspace for gpu_check." >"$tmp/ws/README.md"
	mkdir -p "$tmp/.blitz"
	printf '{"gpu":"%s","workspaces":[{"dir":"%s","open":true}],"active":"%s"}\n' \
		"$mode" "$tmp/ws" "$tmp/ws" >"$tmp/.blitz/desktop.json"
	HOME="$tmp" BLITZ_SOCKET="$tmp/b.sock" "$blitzd" >"$tmp/blitzd.log" 2>&1 &
	service=$!
	HOME="$tmp" BLITZ_SOCKET="$tmp/b.sock" setsid dbus-run-session -- "$app" >"$tmp/app.log" 2>&1 &
	session=$!
	for _ in $(seq 30); do
		pgrep -s "$session" -x WebKitWebProces >/dev/null && break
		sleep 1
	done
	if ! pgrep -s "$session" -x WebKitWebProces >/dev/null; then
		fail "the app didn't start WebKit:"
		sed 's/^/      /' "$tmp/app.log" | tail -5
		cleanup
		continue
	fi
	# 2. What WebKit says it does, in the web process's environment.
	web="$(pgrep -s "$session" -x WebKitWebProces | head -1)"
	mapfile -t env < <(tr '\0' '\n' <"/proc/$web/environ" | grep -E '^(WEBKIT_|GDK_BACKEND=|__NV_)' || true)
	echo "  The app's WebKit environment: ${env[*]:-(none)}"
	report="$(env "${env[@]}" "$probe" "$mode" 2>/dev/null)" || {
		fail "gpu_probe failed: WebKit didn't start or crashed with this policy"
		report=""
	}
	policy="$(field "$report" Policy)"
	painting="$(field "$report" "Threaded rendering")"
	gl="$(field "$report" GL_RENDERER)"
	echo "  WebKit: policy ${policy:-?}, renderer $(field "$report" Renderer | cut -c1-60), painting ${painting:-CPU}, GL ${gl:-none}"
	if [ "$mode" = on ]; then
		if [ -n "$policy" ] && [ "$policy" != never ] && [[ "$painting" == GPU* ]]; then
			pass "draws with the graphics card ($gl)"
		else
			fail "asked for the graphics card, WebKit draws in software (policy ${policy:-unknown})"
		fi
	else
		if [ "$policy" = never ]; then
			pass "draws in software"
		else
			fail "asked for software, WebKit's policy is ${policy:-unknown}"
		fi
	fi

	# 3. The app, idle.
	sleep "$warmup"
	before="$(ticks "$session")"
	sleep "$seconds"
	after="$(ticks "$session")"
	if ! kill -0 "$session" 2>/dev/null; then
		fail "the app exited while idle:"
		sed 's/^/      /' "$tmp/app.log" | tail -5
	fi
	# Per process, and in all: percent of one core over the window.
	usage="$(awk -v hz="$hz" -v s="$seconds" '
		NR == FNR { t0[$1] = $3; next }
		{ d = $3 - t0[$1]; cpu[$2] += d; all += d }
		END {
			for (n in cpu) if (cpu[n] > 0) printf "    %-18s %5.1f%%\n", n, 100 * cpu[n] / hz / s
			printf "total %.1f\n", 100 * all / hz / s
		}' <(echo "$before") <(echo "$after"))"
	total="$(sed -n 's/^total //p' <<<"$usage")"
	echo "  Idle CPU over ${seconds}s (percent of one core):"
	grep -v '^total ' <<<"$usage" | sort -k2 -rn || true
	if awk -v t="$total" -v m="$max_cpu" 'BEGIN { exit !(t <= m) }'; then
		pass "idle: ${total}% (at most ${max_cpu}%)"
	else
		fail "idle: ${total}%, over ${max_cpu}%"
	fi
	cleanup
done

exit "$failed"
