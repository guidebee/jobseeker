#!/bin/bash
# Tests for tools/run_weekly.sh, with a copy of run_jobseeker.sh, a fake binary and a throwaway data root.
# Run:  bash tests/run_weekly_test.sh
set -uo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
WRAPPER="$REPO/tools/run_weekly.sh"
ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT
PASS=0
FAILS=0

PY=""
for c in python3 python; do
  if command -v "$c" >/dev/null 2>&1 && "$c" -c "import sqlite3" >/dev/null 2>&1; then PY="$c"; break; fi
done
[ -n "$PY" ] || { echo "python 3 with sqlite3 is required"; exit 2; }

ok() { PASS=$((PASS + 1)); echo "  ok   $1"; }
bad() { FAILS=$((FAILS + 1)); echo "  FAIL $1"; }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (expected '$3', got '$2')"; fi; }

# The "code folder": the real pipeline script plus a fake binary that records its arguments.
CODE="$ROOT/code"; mkdir -p "$CODE"
cp "$REPO/run_jobseeker.sh" "$CODE/run_jobseeker.sh"
cat > "$CODE/jobseeker.exe" <<'E'
#!/bin/bash
echo "$@" >> "$FAKE_LOG"
echo "MATCH_THRESHOLD=${MATCH_THRESHOLD:-unset}" >> "${FAKE_LOG}.env"
[ -n "${FAKE_SLEEP:-}" ] && sleep "$FAKE_SLEEP"
exit 0
E
chmod +x "$CODE/jobseeker.exe"

DATA="$ROOT/data"; mkdir -p "$DATA/alice/configs" "$DATA/bob/configs"
for p in alice bob; do
  printf 'USER_EMAIL=%s@example.com\nMINIMAX_API_KEY=dummy\n' "$p" > "$DATA/$p/.env"
  printf 'profile:\n  name: %s\n' "$p" > "$DATA/$p/configs/config.yaml"
  "$PY" - "$DATA/$p/jobseeker.db" "$p" <<'E'
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.execute("""CREATE TABLE jobs (id INTEGER PRIMARY KEY, title TEXT, company TEXT, location TEXT, salary TEXT, url TEXT,
  match_score INTEGER, analysis_pros TEXT, analysis_cons TEXT, analysis TEXT, is_analyzed INTEGER, deleted_at TEXT, emailed_at TEXT, source TEXT, job_type TEXT)""")
c.execute("INSERT INTO jobs VALUES (1,?,?,?,?,?,88,'p','c','a',1,NULL,NULL,'seek','permanent')", ('SECRET-TITLE-' + sys.argv[2], 'Acme', 'Melbourne', '', 'https://example.com/secret-link'))
c.commit()
E
done

run() { # run [args...] -> $RC, $OUT (stdout and stderr)
  OUT="$(env JOBSEEKER_DATA_ROOT="$DATA" JOBSEEKER_CODE="$CODE" FAKE_LOG="$ROOT/calls.log" bash "$WRAPPER" "$@" 2>&1)"
  RC=$?
}
nonzero() { [ "$RC" -ne 0 ] && echo fail || echo ran; }
sent_count() { "$PY" -c "import sqlite3,sys;print(sqlite3.connect(sys.argv[1]).execute('select count(*) from jobs where emailed_at is not null').fetchone()[0])" "$DATA/$1/jobseeker.db"; }

echo "argument rules"
run;                         check "no argument is refused" "$(nonzero)" "fail"
run alice bob;               check "two arguments are refused" "$(nonzero)" "fail"
run alice --config x;        check "an extra option is refused" "$(nonzero)" "fail"
for bad_name in Alice alice1 al-ice "../alice" "alice/.." "" "alice bob" "alice;ls"; do
  run "$bad_name"; check "bad name '$bad_name' is refused" "$(nonzero)" "fail"
done
run carol;                   check "a person without a folder is refused" "$(nonzero)" "fail"
check "no folder was created for the refused person" "$([ -e "$DATA/carol" ] && echo yes || echo no)" "no"

echo "a normal run for alice"
: > "$ROOT/calls.log"
run alice
check "exits 0" "$RC" "0"
LOGS="$(ls "$DATA/alice/runs"/*.log 2>/dev/null | wc -l | tr -d ' ')"
check "writes one log in her runs folder" "$LOGS" "1"
check "the log holds the shortlist" "$(grep -c 'SECRET-TITLE-alice' "$DATA"/alice/runs/*.log | tr -d ' ')" "1"
check "the terminal receipt shows no job title" "$(printf '%s' "$OUT" | grep -c 'SECRET-TITLE')" "0"
check "the terminal receipt shows no link" "$(printf '%s' "$OUT" | grep -c 'secret-link')" "0"
check "the terminal receipt shows no email address" "$(printf '%s' "$OUT" | grep -c '@')" "0"
case "$OUT" in *"Found 1 new recommended"*) ok "the receipt gives the count" ;; *) bad "the receipt gives the count: $OUT" ;; esac
check "nothing marked as sent (dry run)" "$(sent_count alice)" "0"
CALLS="$(cat "$ROOT/calls.log")"
case "$CALLS" in *"--config $DATA/alice/configs/config.yaml --database $DATA/alice/jobseeker.db"*) ok "uses alice config and database" ;; *) bad "arguments: $CALLS" ;; esac
check "never touches bob" "$(ls "$DATA/bob/runs" 2>/dev/null | wc -l | tr -d ' ')" "0"
check "bob has nothing marked either" "$(sent_count bob)" "0"

echo "failure is reported"
mv "$DATA/alice/.env" "$DATA/alice/.env.off"
run alice
check "a failing run exits non-zero" "$(nonzero)" "fail"
case "$OUT" in *"log file"*) ok "still names the log file" ;; *) bad "still names the log file: $OUT" ;; esac
mv "$DATA/alice/.env.off" "$DATA/alice/.env"

echo "delivery marker"
mkdir -p "$ROOT/tools/delivery"
runm() { OUT="$(env JOBSEEKER_DATA_ROOT="$DATA" JOBSEEKER_CODE="$CODE" JOBSEEKER_TOOLS="$ROOT/tools" FAKE_LOG="$ROOT/calls.log" bash "$WRAPPER" "$@" 2>&1)"; RC=$?; }
runm bob
check "without a marker the run is a dry run" "$(printf '%s' "$OUT" | grep -c 'delivery: off')" "1"
check "and nothing is marked as sent" "$(sent_count bob)" "0"
touch "$ROOT/tools/delivery/alice.on"
runm bob
check "another person's marker does not enable bob" "$(printf '%s' "$OUT" | grep -c 'delivery: off')" "1"
check "bob still has nothing marked" "$(sent_count bob)" "0"
runm alice
check "with the marker the run exits 0" "$RC" "0"
check "the receipt says delivery is on" "$(printf '%s' "$OUT" | grep -c 'delivery: digest')" "1"
check "the receipt names the digest file" "$(printf '%s' "$OUT" | grep -c 'Digest written: runs/digest-')" "1"
check "a digest file is in alice's runs folder" "$(ls "$DATA/alice/runs" | grep -c '^digest-')" "1"
check "the digest holds the listing" "$(grep -c 'SECRET-TITLE-alice' "$DATA"/alice/runs/digest-*.md | tr -d ' ')" "1"
check "the receipt still shows no job title" "$(printf '%s' "$OUT" | grep -c 'SECRET-TITLE')" "0"
check "the receipt still shows no link" "$(printf '%s' "$OUT" | grep -c 'secret-link')" "0"
check "alice's listing is marked as sent" "$(sent_count alice)" "1"

echo "one run at a time"
L="$DATA/alice/runs/.lock"
rm -rf "$L"
LOGS_BEFORE="$(ls "$DATA/alice/runs"/*.log 2>/dev/null | wc -l | tr -d ' ')"
( env JOBSEEKER_DATA_ROOT="$DATA" JOBSEEKER_CODE="$CODE" JOBSEEKER_TOOLS="$ROOT/tools" FAKE_LOG="$ROOT/calls.log" FAKE_SLEEP=2 bash "$WRAPPER" alice > "$ROOT/first.out" 2>&1; echo $? > "$ROOT/first.rc" ) &
FIRST=$!
sleep 1.5
check "the lock exists while a run is active" "$([ -d "$L" ] && echo yes || echo no)" "yes"
runm alice
check "a second start is refused with exit code 3" "$RC" "3"
case "$OUT" in *"already active"*"Nothing was started"*) ok "and says a run is already active" ;; *) bad "refusal message: $OUT" ;; esac
check "the refused start made no log file of its own" "$(ls "$DATA/alice/runs"/*.log 2>/dev/null | wc -l | tr -d ' ')" "$((LOGS_BEFORE + 1))"
check "and did not remove the running run's lock" "$([ -d "$L" ] && echo yes || echo no)" "yes"
check "another person is not blocked" "$([ -d "$DATA/bob/runs/.lock" ] && echo locked || echo free)" "free"
wait "$FIRST"
check "the first run still finished normally" "$(cat "$ROOT/first.rc")" "0"
check "the lock is released when a run ends" "$([ -e "$L" ] && echo yes || echo no)" "no"
runm alice
check "so the next run starts" "$RC" "0"

echo "a lock left by a crash is taken over"
mkdir -p "$L"; echo 999999 > "$L/pid"; echo "2026-11-21 00:00:00" > "$L/started"
runm alice
check "a lock whose process is gone is taken over" "$RC" "0"
check "and released afterwards" "$([ -e "$L" ] && echo yes || echo no)" "no"
mkdir -p "$L"; echo $$ > "$L/pid"; touch -d "5 hours ago" "$L"
runm alice
check "a lock older than four hours is taken over even if its process id is alive" "$RC" "0"
mv "$DATA/alice/.env" "$DATA/alice/.env.off"
runm alice
check "a failing run exits non-zero" "$([ "$RC" -ne 0 ] && echo fail || echo ran)" "fail"
check "and still releases the lock" "$([ -e "$L" ] && echo yes || echo no)" "no"
mv "$DATA/alice/.env.off" "$DATA/alice/.env"


echo "owner settings (the score a listing needs)"
mkdir -p "$ROOT/tools/settings"
thr() { grep -h '^MATCH_THRESHOLD=' "$ROOT/calls.log.env" | tail -1; }
rm -f "$ROOT/calls.log.env"; runm alice
check "without a settings file the analyzer threshold is left alone" "$(thr)" "MATCH_THRESHOLD=unset"
printf '# the owner chose 80
MIN_SCORE=80
' > "$ROOT/tools/settings/alice.env"
rm -f "$ROOT/calls.log.env"; runm alice
check "a settings file sets the analyzer threshold" "$(thr)" "MATCH_THRESHOLD=80"
check "and the receipt says so" "$(printf '%s' "$OUT" | grep -c 'minimum score: 80 (settings file)')" "1"
printf 'MIN_SCORE=80
' > "$ROOT/tools/settings/alice.env"
runm alice; check "Windows line endings are accepted" "$RC" "0"
for badline in 'MIN_SCORE=0' 'MIN_SCORE=101' 'MIN_SCORE=8x' 'MIN_SCORE=80; rm -rf x' 'MIN_SCORE=$(id)' 'OTHER=1' 'MIN_SCORE='; do
  printf '%s
' "$badline" > "$ROOT/tools/settings/alice.env"
  rm -f "$ROOT/calls.log.env"; runm alice
  check "bad settings line '$badline' stops the run before anything starts" "$(nonzero)$([ -e "$ROOT/calls.log.env" ] && echo ' started')" "fail"
done
check "a bad settings file leaves no lock behind" "$([ -e "$L" ] && echo yes || echo no)" "no"
: > "$ROOT/tools/settings/alice.env"
runm alice; check "an empty settings file is refused" "$(nonzero)" "fail"
rm -f "$ROOT/tools/settings/alice.env"
echo
echo "$PASS passed, $FAILS failed"
[ "$FAILS" -eq 0 ]
