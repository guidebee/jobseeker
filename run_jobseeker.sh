#!/bin/bash
# run_jobseeker.sh: full pipeline for ONE person: scan, analyze, then email the new recommended jobs.
#
# Usage (manual, one person):
#   JOBSEEKER_HOME=/path/to/data/alice ./run_jobseeker.sh
#
# Usage (scheduler): run the same command from cron or the Windows Task Scheduler.
#
# Where things live:
#   JOBSEEKER_DIR   the code folder (this repository). Holds the jobseeker binary. Never holds personal data.
#   JOBSEEKER_HOME  the person's data folder (default: JOBSEEKER_DIR, the old single-user layout). The script runs from
#                   here, so it must contain:  .env, configs/config.yaml, resumes/, and it creates jobseeker.db.
#   One folder per person; never share one between people.
#
# Settings (environment variables):
#   SEND_EMAIL   true to email the shortlist and mark those jobs as sent. Default false: a dry run that prints the
#                shortlist and changes nothing, so the same jobs show up on the next run.
#   DELIVERY     digest: write the shortlist as runs/digest-<stamp>.md in the person's folder, then mark those jobs as sent.
#                Someone (the Agent Hub job-hunter) relays the digest. The wrapper tools/run_weekly.sh sets it only when the
#                owner has switched delivery on for that person. Default: none (the dry run above).
#   EMAIL_SCRIPT path of the Node.js sender (required when SEND_EMAIL=true), TO the recipient, SUBJECT the subject.
#   MIN_SCORE    minimum match score to include (whole number, default 70).
#   JOBSEEKER_BIN  the binary to run (default: $JOBSEEKER_DIR/jobseeker.exe).
#
# Required in the person's .env: USER_EMAIL and MINIMAX_API_KEY (used by analyze). CLAUDE_API_KEY is only needed for
# the checkjd and tailorcv commands, not for this script.

set -euo pipefail

JOBSEEKER_DIR="$(cd "$(dirname "$0")" && pwd)"
JOBSEEKER_BIN="${JOBSEEKER_BIN:-${JOBSEEKER_DIR}/jobseeker.exe}"
JOBSEEKER_HOME="${JOBSEEKER_HOME:-${JOBSEEKER_DIR}}"
SEND_EMAIL="${SEND_EMAIL:-false}"
EMAIL_SCRIPT="${EMAIL_SCRIPT:-}"
TO="${TO:-}"
SUBJECT="${SUBJECT:-Jobseeker: Recommended Jobs Update}"
MIN_SCORE="${MIN_SCORE:-70}"

fail() { echo "[jobseeker] ERROR: $*" >&2; exit 1; }
log() { echo "[jobseeker] $*"; }

# ─── Preflight: check everything before touching any data ─────────────────────
[ -d "$JOBSEEKER_HOME" ] || fail "JOBSEEKER_HOME does not exist: $JOBSEEKER_HOME"
JOBSEEKER_HOME="$(cd "$JOBSEEKER_HOME" && pwd)"
DB_PATH="${JOBSEEKER_HOME}/jobseeker.db"
[[ "$MIN_SCORE" =~ ^[0-9]+$ ]] || fail "MIN_SCORE must be a whole number, got: $MIN_SCORE"
[ -f "$JOBSEEKER_BIN" ] || fail "binary not found: $JOBSEEKER_BIN (build it: go build -o jobseeker.exe ./cmd/jobseeker)"
[ -f "$JOBSEEKER_HOME/.env" ] || fail ".env not found in $JOBSEEKER_HOME"
[ -f "$JOBSEEKER_HOME/configs/config.yaml" ] || fail "configs/config.yaml not found in $JOBSEEKER_HOME (copy configs/config.example.yaml from the repository)"

PYTHON=""
for candidate in python3 python; do
  if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c "import sqlite3" >/dev/null 2>&1; then PYTHON="$candidate"; break; fi
done
[ -n "$PYTHON" ] || fail "python 3 with sqlite3 is required (python3 or python on PATH)"

set -a
# shellcheck disable=SC1091
source "$JOBSEEKER_HOME/.env"
set +a
[ -n "${USER_EMAIL:-}" ] || fail "USER_EMAIL is not set in $JOBSEEKER_HOME/.env"
[ -n "${MINIMAX_API_KEY:-}" ] || fail "MINIMAX_API_KEY is not set in $JOBSEEKER_HOME/.env (analyze needs it)"

if [ "$SEND_EMAIL" = "true" ]; then
  command -v node >/dev/null 2>&1 || fail "SEND_EMAIL=true needs node on PATH"
  [ -n "$EMAIL_SCRIPT" ] && [ -f "$EMAIL_SCRIPT" ] || fail "SEND_EMAIL=true needs EMAIL_SCRIPT to be an existing file (got: ${EMAIL_SCRIPT:-nothing})"
  case "$TO" in
    ""|*@example.com|*@example.org|*@example.net) fail "SEND_EMAIL=true needs TO set to a real address (got: ${TO:-nothing})" ;;
  esac
fi

cd "$JOBSEEKER_HOME"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

log "============================================"
log "Starting pipeline at $(date '+%Y-%m-%d %H:%M:%S') for ${USER_EMAIL}"
log "Data folder: ${JOBSEEKER_HOME}   Send email: ${SEND_EMAIL}"
log "============================================"

mark_sent() {
  log "Marking ${COUNT} job(s) as sent..."
  "$PYTHON" - "$ROWS" "$DB_PATH" <<'PYEOF'
import datetime, json, sqlite3, sys
ids = [r[0] for r in json.load(open(sys.argv[1], encoding="utf-8"))]
now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
conn = sqlite3.connect(sys.argv[2])
conn.executemany("UPDATE jobs SET emailed_at = ? WHERE id = ?", [(now, i) for i in ids])
conn.commit()
conn.close()
print("Marked %d job(s) as sent at %s." % (len(ids), now))
PYEOF
}

run() { "$JOBSEEKER_BIN" --config "$JOBSEEKER_HOME/configs/config.yaml" --database "$DB_PATH" "$@"; }

# ─── Step 1: Scan job boards ──────────────────────────────────────────────────
log "Step 1/3: Scanning job boards..."
run scan
log "Scan complete."

# ─── Step 2: AI analysis ──────────────────────────────────────────────────────
log "Step 2/3: Running AI analysis..."
run analyze
log "Analysis complete."

# ─── Step 3: New recommended jobs (not yet sent) ──────────────────────────────
log "Step 3/3: Querying new recommended jobs (score >= ${MIN_SCORE}, not yet sent)..."
ROWS="$WORK/rows.json"
"$PYTHON" - "$DB_PATH" "$MIN_SCORE" "$ROWS" <<'PYEOF'
import json, sqlite3, sys
db, min_score, out = sys.argv[1], int(sys.argv[2]), sys.argv[3]
conn = sqlite3.connect(db)
rows = conn.execute(
    """SELECT id, title, company, location, salary, url, match_score, analysis_pros, analysis_cons, analysis, source, job_type
       FROM jobs
       WHERE is_analyzed = 1 AND match_score >= ? AND deleted_at IS NULL AND emailed_at IS NULL
       ORDER BY match_score DESC""",
    (min_score,),
).fetchall()
conn.close()
json.dump(rows, open(out, "w", encoding="utf-8"))
PYEOF
COUNT="$("$PYTHON" -c "import json,sys; print(len(json.load(open(sys.argv[1], encoding='utf-8'))))" "$ROWS")"
log "Found ${COUNT} new recommended job(s)."

if [ "$COUNT" -eq 0 ]; then
  log "No new recommended jobs: nothing to send."
  log "Pipeline finished at $(date '+%Y-%m-%d %H:%M:%S')"
  exit 0
fi

# ─── Print the shortlist (always) ─────────────────────────────────────────────
"$PYTHON" - "$ROWS" <<'PYEOF'
import json, sys
rows = json.load(open(sys.argv[1], encoding="utf-8"))
print("\nTop recommended jobs:")
for _id, title, company, location, salary, url, score, pros, cons, analysis, source, job_type in rows[:10]:
    sal = " | %s" % salary if salary else ""
    print("  [%s/100] %s @ %s (%s%s)" % (score, title, company, location, sal))
    print("          %s" % url)
print()
PYEOF

# ─── Digest delivery: write the digest file, then mark as sent; the job-hunter relays the file ───
if [ "${DELIVERY:-none}" = "digest" ]; then
  mkdir -p "$JOBSEEKER_HOME/runs"
  DIGEST_NAME="digest-${JOBSEEKER_RUN_STAMP:-$(date +%Y%m%d-%H%M%S)}.md"
  "$PYTHON" - "$ROWS" "$JOBSEEKER_HOME/runs/$DIGEST_NAME" <<'PYEOF'
import json, re, sys
rows = json.load(open(sys.argv[1], encoding="utf-8"))


def md(value):
    """Plain text made safe for Markdown: scraped text must not become formatting, links or HTML."""
    text = re.sub(r"\s+", " ", "" if value is None else str(value)).strip()
    return re.sub(r"([\\`*_{}\[\]()<>#+!|~])", r"\\\1", text)


def points(raw):
    try:
        data = json.loads(raw) if raw else []
    except ValueError:
        data = [raw]
    if isinstance(data, str):
        data = [data]
    return [str(x).strip() for x in data if str(x).strip()] if isinstance(data, list) else []


def link(url):
    url = "" if url is None else str(url)
    return url.replace("(", "%28").replace(")", "%29").replace(" ", "%20") if url.startswith(("https://", "http://")) else ""


out = ["# Jobseeker: %d new match%s\n" % (len(rows), "" if len(rows) == 1 else "es"),
       "Listings that scored well against the resume and were not sent before, strongest first. Public job adverts only.\n"]
for _id, title, company, location, salary, url, score, pros, cons, analysis, source, job_type in rows:
    out.append("## %s (%s/100)\n" % (md(title) or "Untitled", score))
    facts = [md(x) for x in (company, location, salary, source, job_type) if x]
    if facts:
        out.append(" · ".join(facts) + "\n")
    if link(url):
        out.append("[Open the advert](%s)\n" % link(url))
    for point in points(pros)[:4]:
        out.append("- Pro: " + md(point))
    for point in points(cons)[:4]:
        out.append("- Con: " + md(point))
    out.append("")
open(sys.argv[2], "w", encoding="utf-8").write("\n".join(out))
PYEOF
  log "Digest written: runs/${DIGEST_NAME} (${COUNT} listing(s))"
  mark_sent
  log "Pipeline finished at $(date '+%Y-%m-%d %H:%M:%S')"
  exit 0
fi

if [ "$SEND_EMAIL" != "true" ]; then
  log "SEND_EMAIL is not true: dry run. Nothing was sent and nothing was marked, so these jobs will appear again."
  log "Pipeline finished at $(date '+%Y-%m-%d %H:%M:%S')"
  exit 0
fi

# ─── Build the HTML email (every scraped value is escaped) ────────────────────
log "Building HTML email..."
HTML_FILE="$WORK/digest.html"
"$PYTHON" - "$ROWS" "$HTML_FILE" <<'PYEOF'
import html, json, sys
rows = json.load(open(sys.argv[1], encoding="utf-8"))
esc = lambda v: html.escape("" if v is None else str(v))
def link(url):
    url = "" if url is None else str(url)
    return html.escape(url, quote=True) if url.startswith(("https://", "http://")) else "#"

out = ["""<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><style>
body { font-family: Arial, sans-serif; max-width: 900px; margin: 0 auto; padding: 20px; }
h1 { color: #333; }
.job { border: 1px solid #ddd; border-radius: 8px; padding: 16px; margin-bottom: 16px; }
.score-high { border-left: 5px solid #28a745; }
.score-mid  { border-left: 5px solid #ffc107; }
.badge { display:inline-block; padding:2px 8px; border-radius:12px; font-size:12px; font-weight:bold; }
.badge-high { background:#28a745; color:#fff; }
.badge-mid  { background:#ffc107; color:#000; }
.pros { color: #28a745; }
.cons { color: #dc3545; }
a { color: #007bff; }
</style></head>
<body>
<h1>Jobseeker Recommended Jobs</h1>
<p>Found <strong>%d</strong> new recommended jobs matching your profile.</p>
""" % len(rows)]
for _id, title, company, location, salary, url, score, pros, cons, analysis, source, job_type in rows:
    strong = score >= 85
    sal = " &middot; %s" % esc(salary) if salary else ""
    out.append("""
<div class="job %s">
  <h2><a href="%s">%s</a> <span class="badge %s">%s/100</span></h2>
  <p><strong>%s</strong> &middot; %s%s</p>
  <p>%s</p>
  <p class="pros">Pros: %s</p>
  <p class="cons">Cons: %s</p>
</div>""" % ("score-high" if strong else "score-mid", link(url), esc(title), "badge-high" if strong else "badge-mid",
             esc(score), esc(company), esc(location), sal, esc(analysis), esc(pros) or "N/A", esc(cons) or "N/A"))
out.append("</body></html>")
open(sys.argv[2], "w", encoding="utf-8").write("".join(out))
PYEOF

# ─── Send, then mark as sent only if sending succeeded (set -e stops here on failure) ───
log "Sending email to ${TO}..."
node "$EMAIL_SCRIPT" --to "$TO" --subject "$SUBJECT" --html --body-file "$HTML_FILE"
log "Email sent."

mark_sent

log "============================================"
log "Pipeline done! ${COUNT} job(s) sent."
log "Finished at $(date '+%Y-%m-%d %H:%M:%S')"
log "============================================"
