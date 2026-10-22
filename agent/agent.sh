#!/bin/bash
# SPDX-License-Identifier: MIT
# Roost agent: sends a metrics heartbeat to the hub every INTERVAL seconds.
# Works on macOS (bash 3.2) and Linux. Needs only curl and standard system tools.
set -u

AGENT_VERSION="1.2.0"
CONF="${ROOST_AGENT_CONF:-$HOME/.config/roost-agent/env}"
# shellcheck disable=SC1090
[ -f "$CONF" ] && . "$CONF"
: "${SERVER_URL:?SERVER_URL is not set (expected in $CONF)}"
: "${TOKEN:?TOKEN is not set (expected in $CONF)}"
INTERVAL="${INTERVAL:-15}"
OS="$(uname -s)"
export PATH="$PATH:/usr/local/bin:/opt/homebrew/bin:/usr/sbin:/sbin"
if [ "$OS" = Linux ]; then PAGE_SIZE=$(getconf PAGESIZE 2>/dev/null); fi
PAGE_SIZE="${PAGE_SIZE:-4096}"

json_str() {
  local s="$1"
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/ }
  s=${s//$'\r'/}
  s=${s//$'\t'/ }
  printf '"%s"' "$s"
}

# Prints the value if it is a number, otherwise JSON null.
num() {
  if [[ "${1:-}" =~ ^-?[0-9]+([.][0-9]+)?$ ]]; then printf '%s' "$1"; else printf 'null'; fi
}

# Per-process usage, shared by the macOS and Linux collectors: add() one process at a time,
# then emit() groups them, keeps the heaviest by CPU and by memory, and prints the heartbeat's
# "usage" object. Kept to POSIX awk (BWK on macOS, mawk/gawk/busybox on Linux): no gensub,
# asort, length(array), interval expressions or backslashes in gsub replacements.
USAGE_AWK='
function jstr(s,    n, a, i, o) {
  gsub(/[\001-\037\177]/, " ", s)
  n = split(s, a, "\\"); o = a[1]; for (i = 2; i <= n; i++) o = o "\\\\" a[i]; s = o
  n = split(s, a, "\""); o = a[1]; for (i = 2; i <= n; i++) o = o "\\\"" a[i]
  return "\"" o "\""
}
function add(pid, name, user, cpu, mem, group, cmd) {
  N++; PID[N] = pid; PN[N] = name; PU[N] = user; PC[N] = cpu; PM[N] = mem; PG[N] = group; PX[N] = cmd
}
# Linux only (macOS gets the user from ps); UNAME is filled from /etc/passwd there.
function userof(p,    f, line, a, u) {
  f = "/proc/" p "/status"; u = ""
  while ((getline line < f) > 0) if (line ~ /^Uid:/) { split(line, a, " "); u = a[2]; break }
  close(f)
  return (u in UNAME) ? UNAME[u] : u
}
# Marks in CHOSEN the indexes of the cnt largest non-zero val[1..n].
function pick(n, val, cnt,    k, i, best) {
  split("", USED)
  for (k = 0; k < cnt; k++) {
    best = 0
    for (i = 1; i <= n; i++) if (!(i in USED) && val[i] > 0 && (!best || val[i] > val[best])) best = i
    if (!best) break
    USED[best] = 1; CHOSEN[best] = 1
  }
}
function topnames(key,    i, k, nm, best, found, out) {
  split("", SCORE)
  for (i = 1; i <= N; i++) if (PG[i] == key) SCORE[PN[i]] += int(PC[i] * 10) * 1e13 + PM[i] + 1
  out = ""
  for (k = 0; k < 3; k++) {
    found = 0
    for (nm in SCORE) if (!found || SCORE[nm] > SCORE[best]) { best = nm; found = 1 }
    if (!found) break
    out = out (k ? "," : "") jstr(best)
    delete SCORE[best]
  }
  return out
}
function emit(    i, g, key, sep, tc, tm, u) {
  for (i = 1; i <= N; i++) {
    tc += PC[i]; tm += PM[i]; key = PG[i]
    if (!(key in GI)) { GI[key] = ++G; GK[G] = key }
    g = GI[key]; GC[g] += PC[i]; GM[g] += PM[i]; GN[g]++
  }
  split("", CHOSEN); pick(G, GC, 12); pick(G, GM, 12)
  printf "{\"groups\":["
  sep = ""
  for (g = 1; g <= G; g++) if (g in CHOSEN) {
    printf "%s[%s,%.2f,%.0f,%d,[%s]]", sep, jstr(GK[g]), GC[g], GM[g], GN[g], topnames(GK[g])
    sep = ","
  }
  split("", CHOSEN); pick(N, PC, 12); pick(N, PM, 12)
  printf "],\"procs\":["
  sep = ""
  for (i = 1; i <= N; i++) if (i in CHOSEN) {
    u = PU[i]
    if (u == "" && LINUX) u = userof(PID[i])
    printf "%s[%d,%s,%s,%.2f,%.0f,%s,%s]", sep, PID[i], jstr(PN[i]), jstr(u), PC[i], PM[i], jstr(PG[i]), jstr(substr(PX[i], 1, 160))
    sep = ","
  }
  printf "],\"total\":[%.2f,%.0f,%d]}\n", tc, tm, N
}
'

collect_macos() {
  cpu=$(top -l 2 -n 0 -s 1 2>/dev/null | awk '/CPU usage/ { idle=$7 } END { gsub("%", "", idle); if (idle != "") printf "%.1f", 100 - idle }')
  cores=$(sysctl -n hw.ncpu 2>/dev/null)
  mem_total=$(sysctl -n hw.memsize 2>/dev/null)
  local page
  page=$(sysctl -n hw.pagesize 2>/dev/null)
  mem_used=$(vm_stat 2>/dev/null | awk -v page="${page:-4096}" -F: '
    /Pages active/ { gsub(/[^0-9]/, "", $2); a = $2 }
    /Pages wired down/ { gsub(/[^0-9]/, "", $2); w = $2 }
    /Pages occupied by compressor/ { gsub(/[^0-9]/, "", $2); c = $2 }
    END { printf "%.0f", (a + w + c) * page }')
  read -r swap_total swap_used <<EOF
$(sysctl -n vm.swapusage 2>/dev/null | awk '{ t=$3; u=$6; gsub("M", "", t); gsub("M", "", u); printf "%.0f %.0f", t * 1048576, u * 1048576 }')
EOF
  local df_line
  df_line=$(df -k /System/Volumes/Data 2>/dev/null | tail -n 1)
  [ -z "$df_line" ] && df_line=$(df -k / | tail -n 1)
  disk_total=$(echo "$df_line" | awk '{ printf "%.0f", $2 * 1024 }')
  # APFS volumes share one container, so "used" is everything that isn't free.
  disk_used=$(echo "$df_line" | awk '{ printf "%.0f", ($2 - $4) * 1024 }')
  read -r load1 load5 load15 <<EOF
$(sysctl -n vm.loadavg 2>/dev/null | awk '{ print $2, $3, $4 }')
EOF
  local boot
  boot=$(sysctl -n kern.boottime 2>/dev/null | sed -E 's/^[{] sec = ([0-9]+).*/\1/')
  uptime_s=$(( $(date +%s) - ${boot:-$(date +%s)} ))
  procs=$(ps -A 2>/dev/null | wc -l | tr -d ' ')
  temp=""

  battery="null"
  local batt pct status source
  batt=$(pmset -g batt 2>/dev/null)
  if echo "$batt" | grep -q 'InternalBattery'; then
    pct=$(echo "$batt" | grep -Eo '[0-9]+%' | head -n 1 | tr -d '%')
    status=$(echo "$batt" | grep -Eo '[0-9]+%; [a-zA-Z ]+' | head -n 1 | sed -E 's/^[0-9]+%; //')
    source=$(echo "$batt" | head -n 1)
    local charging=false on_ac=false
    [ "$status" = "charging" ] && charging=true
    echo "$source" | grep -q "AC Power" && on_ac=true
    [ -n "$pct" ] && battery="{\"pct\":$pct,\"charging\":$charging,\"onAc\":$on_ac}"
  fi

  local iface
  iface=$(route -n get default 2>/dev/null | awk '/interface:/ { print $2 }')
  read -r net_rx net_tx <<EOF
$( [ -n "$iface" ] && netstat -ibn -I "$iface" 2>/dev/null | awk 'NR > 1 && $3 ~ /^<Link/ { print $7, $10; exit }')
EOF

  os_name="macOS"
  os_version="macOS $(sw_vers -productVersion 2>/dev/null)"
  model=$(sysctl -n hw.model 2>/dev/null)
  host=$(scutil --get ComputerName 2>/dev/null || hostname)
  usage=$(macos_usage)
}

# ps reports %CPU of one core (a decaying average) and RSS in KB. Apps are grouped by their
# outermost .app bundle, so Chrome's helpers count as "Google Chrome"; anything else by executable.
macos_usage() {
  LC_ALL=C ps -Ao pid=,pcpu=,rss=,user=,comm= 2>/dev/null | LC_ALL=C awk -v cores="${cores:-1}" "$USAGE_AWK"'
{
  c = $0
  sub(/^ *[0-9]+ +[0-9.]+ +[0-9]+ +[^ ]+ +/, "", c)
  base = c; sub(/.*\//, "", base)
  i = index(c, ".app/")
  if (c == "kernel_task") { kind = "kernel"; g = "Kernel" }
  else if (i) { g = substr(c, 1, i - 1); sub(/.*\//, "", g); kind = "app" }
  else { kind = "other"; g = base }
  add($1, base, $4, $2 / (cores > 0 ? cores : 1), $3 * 1024, kind ":" g, c)
}
END { emit() }'
}

cpu_sample() {
  awk '/^cpu / { idle = $5 + $6; total = 0; for (i = 2; i <= 9; i++) total += $i; print total, idle }' /proc/stat
}

collect_linux() {
  local t1 i1 t2 i2 p1 p2
  read -r t1 i1 <<EOF
$(cpu_sample)
EOF
  p1=$(cat /proc/[0-9]*/stat 2>/dev/null)
  sleep 1
  read -r t2 i2 <<EOF
$(cpu_sample)
EOF
  p2=$(cat /proc/[0-9]*/stat 2>/dev/null)
  usage=$(linux_usage "$p1" "$p2" $((t2 - t1)))
  cpu=$(awk -v t=$((t2 - t1)) -v i=$((i2 - i1)) 'BEGIN { if (t > 0) printf "%.1f", (1 - i / t) * 100 }')
  cores=$(grep -c '^processor' /proc/cpuinfo)
  read -r mem_total mem_used swap_total swap_used <<EOF
$(awk '/^MemTotal:/ { t = $2 } /^MemAvailable:/ { a = $2 } /^SwapTotal:/ { st = $2 } /^SwapFree:/ { sf = $2 }
  END { printf "%.0f %.0f %.0f %.0f", t * 1024, (t - a) * 1024, st * 1024, (st - sf) * 1024 }' /proc/meminfo)
EOF
  read -r disk_total disk_used <<EOF
$(df -kP / | awk 'NR == 2 { printf "%.0f %.0f", $2 * 1024, $3 * 1024 }')
EOF
  read -r load1 load5 load15 _ <<EOF
$(cat /proc/loadavg)
EOF
  uptime_s=$(awk '{ printf "%d", $1 }' /proc/uptime)
  procs=$(awk '{ split($4, a, "/"); print a[2] }' /proc/loadavg)

  temp=""
  local z t type
  for z in /sys/class/thermal/thermal_zone*; do
    [ -r "$z/temp" ] || continue
    type=$(cat "$z/type" 2>/dev/null)
    t=$(cat "$z/temp" 2>/dev/null)
    case "$type" in x86_pkg_temp|cpu*|soc*|coretemp*|acpitz) ;; *) [ -n "$temp" ] && continue ;; esac
    if [ -n "$t" ] && [ "$t" -gt 0 ] 2>/dev/null; then
      t=$(awk -v t="$t" 'BEGIN { printf "%.1f", t / 1000 }')
      temp="$t"
      [ "$type" = "x86_pkg_temp" ] && break
    fi
  done

  battery="null"
  local b sum=0 count=0 charging=false on_ac=false s
  for b in /sys/class/power_supply/BAT*; do
    [ -r "$b/capacity" ] || continue
    sum=$((sum + $(cat "$b/capacity")))
    count=$((count + 1))
    s=$(cat "$b/status" 2>/dev/null)
    [ "$s" = "Charging" ] && charging=true
  done
  for b in /sys/class/power_supply/*; do
    [ -r "$b/online" ] && [ "$(cat "$b/type" 2>/dev/null)" = "Mains" ] && [ "$(cat "$b/online")" = "1" ] && on_ac=true
  done
  [ "$count" -gt 0 ] && battery="{\"pct\":$((sum / count)),\"charging\":$charging,\"onAc\":$on_ac}"

  local iface
  iface=$(ip route show default 2>/dev/null | awk '{ for (i = 1; i <= NF; i++) if ($i == "dev") { print $(i + 1); exit } }')
  read -r net_rx net_tx <<EOF
$( [ -n "$iface" ] && sed 's/:/ /' /proc/net/dev | awk -v ifc="$iface" '$1 == ifc { print $2, $10 }')
EOF

  os_name="linux"
  # shellcheck source=/dev/null
  os_version=$(. /etc/os-release 2>/dev/null && echo "${PRETTY_NAME:-Linux}")
  model=$(cat /sys/devices/virtual/dmi/id/product_name 2>/dev/null)
  host=$(hostname)
}

# Per-process CPU from two /proc/[pid]/stat snapshots taken around the 1 s CPU sleep ($1, $2), as a
# share of the jiffies all CPUs spent in between ($3). Grouped by cgroup the same way the hub does.
linux_usage() {
  {
    printf '%s\n@@\n%s\n@@\n' "$1" "$2"
    # head labels each file ("==> /proc/1/cmdline <=="); /dev/null first so it does even for one pid.
    head -c 300 /dev/null /proc/[0-9]*/cmdline 2>/dev/null | tr '\0' ' '
  } | LC_ALL=C awk -v ticks="$3" -v page="$PAGE_SIZE" "$USAGE_AWK"'
# comm can hold spaces and parentheses, so it runs from the first "(" to the last ")".
function parse(s,    o, f) {
  o = index(s, "(")
  if (!o || !match(s, /\)[^)]*$/)) return 0
  S_pid = substr(s, 1, o - 2) + 0
  S_comm = substr(s, o + 1, RSTART - o - 1)
  if (split(substr(s, RSTART + 2), f, " ") < 22) return 0
  S_flags = f[7]; S_ticks = f[12] + f[13]; S_start = f[20]; S_rss = f[22]
  return 1
}
function cgroup(p,    f, line, v2, sd, i) {
  f = "/proc/" p "/cgroup"; v2 = ""; sd = ""
  while ((getline line < f) > 0) {
    if (substr(line, 1, 3) == "0::") v2 = substr(line, 4)
    else if (i = index(line, ":name=systemd:")) sd = substr(line, i + 14)
  }
  close(f)
  return (v2 != "" && v2 != "/") ? v2 : (sd != "" ? sd : v2)
}
function unit(s) { sub(/\.(service|scope|slice)$/, "", s); gsub(/\\x2d/, "-", s); return s }
function app(s,    o) {
  o = s; s = unit(s); sub(/^app-/, "", s)
  if (match(s, /-[0-9a-f]+$/) && (RLENGTH > 8 || substr(s, RSTART + 1) ~ /^[0-9]+$/)) s = substr(s, 1, RSTART - 1)
  return s == "" ? o : s
}
# Sets KIND and GNAME. Must classify processes the same way the hub does.
function classify(path, kthread, name,    n, i, m, at, id, uid) {
  KIND = "other"; GNAME = name
  if (kthread) { KIND = "kernel"; GNAME = "Kernel"; return }
  n = split(path, SEG, "/"); m = 0
  for (i = 1; i <= n; i++) if (SEG[i] != "") SG[++m] = SEG[i]
  if (!m) return
  for (i = 1; i <= m; i++) {
    id = ""
    if (SG[i] ~ /^(docker|libpod|cri-containerd|crio)-[0-9a-f]+\.scope$/) { id = SG[i]; sub(/\.scope$/, "", id); sub(/.*-/, "", id) }
    else if (SG[i] == "docker" && i < m && SG[i + 1] ~ /^[0-9a-f]+$/) id = SG[i + 1]
    if (length(id) == 64) { KIND = "container"; GNAME = substr(id, 1, 12); return }
  }
  if (SG[1] == "init.scope") { KIND = "service"; GNAME = "systemd"; return }
  if (SG[1] == "user.slice") {
    at = 0
    for (i = 2; i <= m; i++) if (SG[i] ~ /^user@[0-9]+\.service$/) { at = i; break }
    if (at) {
      KIND = "user-service"
      if (at == m || SG[at + 1] == "init.scope") { GNAME = "systemd --user"; return }
      for (i = at + 1; i <= m; i++) if (SG[i] ~ /\.service$/) { GNAME = unit(SG[i]); return }
      if (SG[at + 1] == "app.slice" && at + 2 <= m && SG[at + 2] ~ /^app-.*\.slice$/) { GNAME = app(SG[at + 2]); return }
      GNAME = app(SG[m]); return
    }
    for (i = 2; i <= m; i++) if (SG[i] ~ /^session-[^.]*\.scope$/) {
      uid = "unknown"
      if (SG[2] ~ /^user-[0-9]+\.slice$/) { uid = SG[2]; sub(/^user-/, "", uid); sub(/\.slice$/, "", uid); if (uid in UNAME) uid = UNAME[uid] }
      KIND = "session"; GNAME = "Login sessions (" uid ")"; return
    }
  }
  for (i = 1; i <= m; i++) if (SG[i] ~ /\.service$/) { KIND = "service"; GNAME = unit(SG[i]); return }
  GNAME = app(SG[m])
}
BEGIN {
  LINUX = 1; sec = 1
  while ((getline line < "/etc/passwd") > 0) { split(line, pw, ":"); UNAME[pw[3]] = pw[1] }
  close("/etc/passwd")
}
$0 == "@@" { sec++; next }
sec == 1 { if (parse($0)) { T0[S_pid] = S_ticks; ST0[S_pid] = S_start }; next }
sec == 2 {
  if (parse($0)) {
    m++; ID[m] = S_pid; CO[m] = S_comm; RSS[m] = S_rss
    KT[m] = int(S_flags / 2097152) % 2
    # Not in the first snapshot: it started during the sleep, so all its time counts.
    D[m] = (S_pid in T0 && ST0[S_pid] == S_start) ? S_ticks - T0[S_pid] : S_ticks
  }
  next
}
/^==> \/proc\/[0-9]+\/cmdline <==$/ { cur = $2; sub(/^\/proc\//, "", cur); sub(/\/cmdline$/, "", cur); want = 1; next }
want { CMD[cur] = $0; want = 0 }
END {
  for (j = 1; j <= m; j++) {
    p = ID[j]; name = CO[j]; c = CMD[p]; sub(/ +$/, "", c)
    a0 = c; sub(/ .*/, "", a0); sub(/.*\//, "", a0)
    # comm is cut at 15 characters, and Node renames its main thread; argv[0] reads better then.
    if (!KT[j] && a0 != "" && ((length(name) == 15 && index(a0, name) == 1) || name ~ /^(node-)?MainThread$/)) name = a0
    if (KT[j] || c == "") c = "[" CO[j] "]"
    classify(KT[j] ? "" : cgroup(p), KT[j], name)
    add(p, name, "", ticks > 0 ? D[j] * 100 / ticks : 0, RSS[j] * page, KIND ":" GNAME, c)
  }
  emit()
}'
}

build_payload() {
  usage=null
  case "$OS" in
    Darwin) collect_macos ;;
    Linux) collect_linux ;;
    *) echo "unsupported OS: $OS" >&2; exit 1 ;;
  esac
  # A truncated or empty usage object must not take the rest of the heartbeat down with it.
  case "$usage" in '{"groups":['*']}') ;; *) usage=null ;; esac
  payload=$(printf '{"v":1,"agentVersion":%s,"hostname":%s,"os":%s,"osVersion":%s,"kernel":%s,"model":%s,"arch":%s,"metrics":{"cpu":%s,"cores":%s,"load":[%s,%s,%s],"memUsed":%s,"memTotal":%s,"swapUsed":%s,"swapTotal":%s,"diskUsed":%s,"diskTotal":%s,"netRxTotal":%s,"netTxTotal":%s,"temp":%s,"battery":%s,"uptime":%s,"procs":%s},"usage":%s}' \
    "$(json_str "$AGENT_VERSION")" "$(json_str "$host")" \
    "$(json_str "$os_name")" "$(json_str "$os_version")" "$(json_str "$(uname -r)")" \
    "$(json_str "$model")" "$(json_str "$(uname -m)")" \
    "$(num "$cpu")" "$(num "$cores")" "$(num "$load1")" "$(num "$load5")" "$(num "$load15")" \
    "$(num "$mem_used")" "$(num "$mem_total")" "$(num "$swap_used")" "$(num "$swap_total")" \
    "$(num "$disk_used")" "$(num "$disk_total")" "$(num "${net_rx:-}")" "$(num "${net_tx:-}")" \
    "$(num "$temp")" "$battery" "$(num "$uptime_s")" "$(num "$procs")" "$usage")
}

# Sends one heartbeat. If it fails, says why on stderr, with what the hub answered.
send_heartbeat() {
  local out code body msg
  build_payload
  # The token goes through stdin (-K -) so it never shows up in `ps`. Roost
  # refuses POSTs without an X-Roost header, which pages on other sites can't send.
  out=$(printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" |
    curl -sS -m 10 -K - -w '\n%{http_code}' -X POST -H 'Content-Type: application/json' -H 'X-Roost: agent' \
      --data-binary "$payload" "$SERVER_URL/api/agent/heartbeat") || {
    echo "$(date '+%Y-%m-%d %H:%M:%S') heartbeat failed: couldn't reach $SERVER_URL" >&2
    return 1
  }
  code=${out##*$'\n'}
  case "$code" in 2??) return 0 ;; esac
  body=${out%$'\n'*}
  # Roost's errors look like {"error":"..."}.
  msg=$(printf '%s' "$body" | sed -n 's/^{"error":"\(.*\)"}$/\1/p')
  echo "$(date '+%Y-%m-%d %H:%M:%S') heartbeat failed with HTTP $code: ${msg:-$body}" >&2
  return 1
}

if [ "${1:-}" = "--print" ]; then
  build_payload
  printf '%s\n' "$payload"
  exit 0
fi

if [ "${1:-}" = "--once" ]; then
  send_heartbeat && echo "heartbeat sent to $SERVER_URL"
  exit $?
fi

while true; do
  send_heartbeat
  sleep "$INTERVAL"
done
