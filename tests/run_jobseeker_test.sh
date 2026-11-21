#!/bin/bash
# Tests for run_jobseeker.sh using a fake binary, a fake email sender and a throwaway database.
# Run:  bash tests/run_jobseeker_test.sh
set -uo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$REPO/run_jobseeker.sh"
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

# A fake jobseeker binary: records its arguments, does nothing else.
cat > "$ROOT/fake-jobseeker" <<'E'
#!/bin/bash
echo "$@" >> "$FAKE_LOG"
E
chmod +x "$ROOT/fake-jobseeker"

# A fake email sender: records the call and the HTML; fails when FAKE_MAIL_FAIL=1.
cat > "$ROOT/fake-send.js" <<'E'
const fs = require('fs');
const a = process.argv.slice(2);
const get = (k) => a[a.indexOf(k) + 1];
fs.appendFileSync(process.env.FAKE_MAIL_LOG, get('--to') + '\n');
fs.writeFileSync(process.env.FAKE_MAIL_HTML, fs.readFileSync(get('--body-file'), 'utf8'));
process.exit(process.env.FAKE_MAIL_FAIL === '1' ? 3 : 0);
E

# A person's data folder with one shortlisted job whose text is hostile on purpose.
new_home() {
  local h="$1"
  mkdir -p "$h/configs"
  printf 'USER_EMAIL=test@example.com\nMINIMAX_API_KEY=dummy\n' > "$h/.env"
  printf 'profile:\n  name: Test\n' > "$h/configs/config.yaml"
  "$PY" - "$h/jobseeker.db" <<'E'
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.execute("""CREATE TABLE jobs (id INTEGER PRIMARY KEY, title TEXT, company TEXT, location TEXT, salary TEXT, url TEXT,
  match_score INTEGER, analysis_pros TEXT, analysis_cons TEXT, analysis TEXT, is_analyzed INTEGER, deleted_at TEXT, emailed_at TEXT, source TEXT, job_type TEXT)""")
hostile = 'Dev """ \\\\ \\n <script>alert(1)</script> "quoted" & co'
c.execute("INSERT INTO jobs VALUES (1,?,?,?,?,?,90,'[\"good\", \"better *bold*\"]','[\"bad\"]','fine',1,NULL,NULL,'seek','permanent')", (hostile, 'Acme <b>', 'Melbourne', '', 'javascript:alert(1)'))
c.execute("INSERT INTO jobs VALUES (2,'Plain role','Co','Perth','',  'https://example.com/j/2',75,'[\"p\"]','[\"c\"]','a',1,NULL,NULL,'linkedin','contract')")
c.execute("INSERT INTO jobs VALUES (3,'Low score','Co','Perth','','https://example.com/j/3',40,'p','c','a',1,NULL,NULL,'seek','permanent')")
c.commit()
E
}
sent_count() { "$PY" -c "import sqlite3,sys;print(sqlite3.connect(sys.argv[1]).execute('select count(*) from jobs where emailed_at is not null').fetchone()[0])" "$1/jobseeker.db"; }

run() { # run <home> [VAR=value ...] -> exit code in $RC, output in $OUT
  local h="$1"; shift
  OUT="$(env JOBSEEKER_HOME="$h" JOBSEEKER_BIN="$ROOT/fake-jobseeker" FAKE_LOG="$ROOT/calls.log" \
    FAKE_MAIL_LOG="$ROOT/mail.log" FAKE_MAIL_HTML="$ROOT/mail.html" "$@" bash "$SCRIPT" 2>&1)"
  RC=$?
}

echo "dry run (SEND_EMAIL unset)"
H="$ROOT/a"; new_home "$H"; : > "$ROOT/calls.log"; : > "$ROOT/mail.log"
run "$H"
check "exits 0" "$RC" "0"
check "marks nothing as sent" "$(sent_count "$H")" "0"
check "sends no email" "$(wc -l < "$ROOT/mail.log" | tr -d ' ')" "0"
case "$OUT" in *"dry run"*) ok "says it was a dry run" ;; *) bad "says it was a dry run" ;; esac

echo "second dry run shows the same jobs again"
run "$H"
case "$OUT" in *"Found 2 new recommended"*) ok "still 2 new jobs" ;; *) bad "still 2 new jobs: $OUT" ;; esac

echo "refuses placeholder or missing settings when sending"
run "$H" SEND_EMAIL=true EMAIL_SCRIPT="$ROOT/fake-send.js" TO=your.email@example.com
check "placeholder TO fails" "$([ "$RC" -ne 0 ] && echo fail || echo ran)" "fail"
run "$H" SEND_EMAIL=true EMAIL_SCRIPT=/path/to/send_email.js TO=real@corp.test
check "missing EMAIL_SCRIPT fails" "$([ "$RC" -ne 0 ] && echo fail || echo ran)" "fail"
check "still nothing marked" "$(sent_count "$H")" "0"

echo "a failed send marks nothing"
run "$H" SEND_EMAIL=true EMAIL_SCRIPT="$ROOT/fake-send.js" TO=real@corp.test FAKE_MAIL_FAIL=1
check "exits non-zero" "$([ "$RC" -ne 0 ] && echo fail || echo ran)" "fail"
check "marks nothing after a failed send" "$(sent_count "$H")" "0"

echo "a successful send marks the sent jobs and escapes hostile text"
run "$H" SEND_EMAIL=true EMAIL_SCRIPT="$ROOT/fake-send.js" TO=real@corp.test
check "exits 0" "$RC" "0"
check "marks the 2 shortlisted jobs" "$(sent_count "$H")" "2"
HTML="$(cat "$ROOT/mail.html")"
case "$HTML" in *"<script>"*) bad "script tag escaped" ;; *) ok "script tag escaped" ;; esac
case "$HTML" in *"javascript:"*) bad "unsafe link replaced" ;; *) ok "unsafe link replaced" ;; esac
case "$HTML" in *"&lt;script&gt;"*) ok "text shown, escaped" ;; *) bad "text shown, escaped" ;; esac
run "$H"
case "$OUT" in *"No new recommended"*) ok "next run has nothing new" ;; *) bad "next run has nothing new: $OUT" ;; esac

echo "per-person data folder, apart from the code folder"
B="$ROOT/b"; new_home "$B"; : > "$ROOT/calls.log"
REPO_DB_BEFORE="$(stat -c '%Y %s' "$REPO/jobseeker.db" 2>/dev/null || echo none)"
run "$B"
CALLS="$(cat "$ROOT/calls.log")"
case "$CALLS" in *"--config $B/configs/config.yaml --database $B/jobseeker.db scan"*) ok "scan uses that person's config and database" ;; *) bad "scan arguments: $CALLS" ;; esac
check "writes the database in that person's folder" "$([ -s "$B/jobseeker.db" ] && echo yes || echo no)" "yes"
check "leaves the code folder's database untouched" "$(stat -c '%Y %s' "$REPO/jobseeker.db" 2>/dev/null || echo none)" "$REPO_DB_BEFORE"

echo "preflight"
mkdir -p "$ROOT/empty"
run "$ROOT/empty"
check "missing .env fails" "$([ "$RC" -ne 0 ] && echo fail || echo ran)" "fail"
C="$ROOT/c"; new_home "$C"; printf 'USER_EMAIL=test@example.com\n' > "$C/.env"
run "$C"
case "$OUT" in *MINIMAX_API_KEY*) ok "names the missing MINIMAX_API_KEY" ;; *) bad "names the missing MINIMAX_API_KEY: $OUT" ;; esac
run "$B" MIN_SCORE="70; DROP TABLE jobs"
check "non-numeric MIN_SCORE fails" "$([ "$RC" -ne 0 ] && echo fail || echo ran)" "fail"

echo "digest delivery"
D="$ROOT/d"; new_home "$D"
check "a dry run writes no digest" "$([ -e "$H/runs" ] && ls "$H/runs" | grep -c digest || echo 0)" "0"
run "$D" DELIVERY=digest JOBSEEKER_RUN_STAMP=20261121-120000
check "exits 0" "$RC" "0"
DIGEST="$D/runs/digest-20261121-120000.md"
check "writes the digest in the person folder" "$([ -s "$DIGEST" ] && echo yes || echo no)" "yes"
check "marks the 2 shortlisted jobs as sent" "$(sent_count "$D")" "2"
check "the low-scoring listing is not in the digest" "$(grep -c 'Low score' "$DIGEST")" "0"
check "the strongest listing comes first" "$(grep -n '^## ' "$DIGEST" | head -1 | grep -c '90/100')" "1"
check "scraped HTML does not survive as HTML" "$(grep -c '<script>' "$DIGEST")" "0"
check "scraped HTML is shown escaped" "$(grep -cF '\<script\>' "$DIGEST")" "1"
check "an unsafe link is not made a link" "$(grep -c 'javascript:' "$DIGEST")" "0"
check "a web link is kept as a link" "$(grep -cF '[Open the advert](https://example.com/j/2)' "$DIGEST")" "1"
check "pros are listed and their markdown is escaped" "$(grep -cF 'Pro: better \*bold\*' "$DIGEST")" "1"
check "cons are listed" "$(grep -c 'Con: bad' "$DIGEST")" "1"
check "board and job type are shown" "$(grep -c 'linkedin · contract' "$DIGEST")" "1"
case "$OUT" in *"Digest written: runs/digest-20261121-120000.md"*) ok "the log names the digest" ;; *) bad "the log names the digest: $OUT" ;; esac
run "$D" DELIVERY=digest JOBSEEKER_RUN_STAMP=20261121-130000
case "$OUT" in *"No new recommended"*) ok "the next run has nothing new" ;; *) bad "the next run has nothing new: $OUT" ;; esac
check "and writes no second digest" "$(ls "$D/runs" | grep -c digest)" "1"

echo
echo "$PASS passed, $FAILS failed"
[ "$FAILS" -eq 0 ]
