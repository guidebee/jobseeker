#!/bin/bash
# run_jobseeker.sh — Full pipeline: scan → analyze → email recommended jobs
#
# Usage (manual):
#   chmod +x run_jobseeker.sh
#   ./run_jobseeker.sh
#
# Usage (cron — runs every weekday at 8 AM):
#   crontab -e
#   0 8 * * 1-5 /home/youruser/workspace/jobseeker/run_jobseeker.sh >> /tmp/jobseeker.log 2>&1
#
# Prerequisites:
#   - jobseeker.exe built:  go build -o jobseeker.exe ./cmd/jobseeker
#   - .env configured with USER_EMAIL, MINIMAX_API_KEY, CLAUDE_API_KEY
#   - Profile initialised: ./jobseeker.exe init
#   - Email sender script (Node.js) available at EMAIL_SCRIPT path (optional)
#   - python3 available (for HTML generation and DB update)

set -e

# ─── Configuration ────────────────────────────────────────────────────────────
JOBSEEKER_DIR="$(cd "$(dirname "$0")" && pwd)"
DB_PATH="${JOBSEEKER_DIR}/jobseeker.db"

# Email settings — set SEND_EMAIL=false to skip emailing and just print results
SEND_EMAIL="${SEND_EMAIL:-false}"
EMAIL_SCRIPT="${EMAIL_SCRIPT:-/path/to/send_email.js}"
TO="${TO:-your.email@example.com}"
SUBJECT="${SUBJECT:-Jobseeker: Recommended Jobs Update}"

# Minimum match score to include in email (overrides MATCH_THRESHOLD in .env)
MIN_SCORE="${MIN_SCORE:-70}"
# ──────────────────────────────────────────────────────────────────────────────

cd "$JOBSEEKER_DIR"

# Load .env (API keys, USER_EMAIL, etc.)
if [ -f ".env" ]; then
  set -a && source .env && set +a
else
  echo "[jobseeker] ERROR: .env file not found in $JOBSEEKER_DIR"
  exit 1
fi

echo "[jobseeker] ============================================"
echo "[jobseeker] Starting pipeline at $(date '+%Y-%m-%d %H:%M:%S')"
echo "[jobseeker] ============================================"

# ─── Step 1: Scan job boards ──────────────────────────────────────────────────
echo "[jobseeker] Step 1/3 — Scanning job boards..."
./jobseeker.exe scan
echo "[jobseeker] Scan complete."

# ─── Step 2: AI analysis ──────────────────────────────────────────────────────
echo "[jobseeker] Step 2/3 — Running AI analysis..."
./jobseeker.exe analyze
echo "[jobseeker] Analysis complete."

# ─── Step 3: Query new recommended jobs (not yet emailed) ────────────────────
echo "[jobseeker] Step 3/3 — Querying new recommended jobs (score >= ${MIN_SCORE}, not yet emailed)..."
RECOMMENDED=$(python3 - <<EOF
import sqlite3, json
conn = sqlite3.connect("${DB_PATH}")
cur = conn.cursor()
cur.execute("""
    SELECT id, title, company, location, salary, url, match_score,
           analysis_pros, analysis_cons, analysis
    FROM jobs
    WHERE is_analyzed = 1
      AND match_score >= ${MIN_SCORE}
      AND deleted_at IS NULL
      AND emailed_at IS NULL
    ORDER BY match_score DESC
""")
rows = cur.fetchall()
conn.close()
print(json.dumps(rows))
EOF
)

COUNT=$(python3 -c "import json,sys; print(len(json.loads(sys.stdin.read())))" <<< "$RECOMMENDED")
echo "[jobseeker] Found ${COUNT} new recommended job(s)."

if [ "$COUNT" -eq "0" ]; then
  echo "[jobseeker] No new recommended jobs — nothing to send."
  echo "[jobseeker] Pipeline finished at $(date '+%Y-%m-%d %H:%M:%S')"
  exit 0
fi

# ─── Print summary to stdout (always) ─────────────────────────────────────────
python3 - <<EOF
import json
rows = json.loads("""${RECOMMENDED}""")
print(f"\nTop recommended jobs:")
for r in rows[:10]:
    _id, title, company, location, salary, url, score, pros, cons, analysis = r
    sal = f" | {salary}" if salary else ""
    print(f"  [{score}/100] {title} @ {company} ({location}{sal})")
    print(f"          {url}")
print()
EOF

# ─── Build HTML email ──────────────────────────────────────────────────────────
if [ "$SEND_EMAIL" = "true" ]; then
  echo "[jobseeker] Building HTML email..."
  _TMPFILE=$(mktemp)
  echo "$RECOMMENDED" > "$_TMPFILE"

  HTML=$(python3 - "$_TMPFILE" <<'PYEOF'
import json, sys

rows = json.loads(open(sys.argv[1]).read())

html = """<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><style>
body { font-family: Arial, sans-serif; max-width: 900px; margin: 0 auto; padding: 20px; }
h1 { color: #333; }
.job { border: 1px solid #ddd; border-radius: 8px; padding: 16px; margin-bottom: 16px; }
.score-high { border-left: 5px solid #28a745; }
.score-mid  { border-left: 5px solid #ffc107; }
.score-low  { border-left: 5px solid #dc3545; }
.badge { display:inline-block; padding:2px 8px; border-radius:12px; font-size:12px; font-weight:bold; }
.badge-high { background:#28a745; color:#fff; }
.badge-mid  { background:#ffc107; color:#000; }
.badge-low  { background:#dc3545; color:#fff; }
.pros { color: #28a745; }
.cons { color: #dc3545; }
a { color: #007bff; }
</style></head>
<body>
<h1>Jobseeker Recommended Jobs</h1>
<p>Found <strong>{count}</strong> new recommended jobs matching your profile.</p>
""".replace('{count}', str(len(rows)))

for r in rows:
    _id, title, company, location, salary, url, score, pros, cons, analysis = r
    if score >= 85:
        cls, badge = "score-high", "badge-high"
    elif score >= 70:
        cls, badge = "score-mid", "badge-mid"
    else:
        cls, badge = "score-low", "badge-low"

    sal_str = f' &middot; {salary}' if salary else ''
    html += f"""
<div class="job {cls}">
  <h2><a href="{url}">{title}</a> <span class="badge {badge}">{score}/100</span></h2>
  <p><strong>{company}</strong> &middot; {location}{sal_str}</p>
  <p>{analysis or ''}</p>
  <p class="pros">Pros: {pros or 'N/A'}</p>
  <p class="cons">Cons: {cons or 'N/A'}</p>
</div>"""

html += "</body></html>"
print(html)
PYEOF
  )
  rm -f "$_TMPFILE"

  # ─── Send email ───────────────────────────────────────────────────────────────
  echo "[jobseeker] Sending email to ${TO}..."
  _HTML_FILE=$(mktemp --suffix=.html)
  echo "$HTML" > "$_HTML_FILE"
  node "$EMAIL_SCRIPT" \
    --to "$TO" \
    --subject "$SUBJECT" \
    --html \
    --body-file "$_HTML_FILE"
  rm -f "$_HTML_FILE"
  echo "[jobseeker] Email sent."
fi

# ─── Mark jobs as emailed ─────────────────────────────────────────────────────
echo "[jobseeker] Marking ${COUNT} job(s) as emailed..."
_MARK_TMPFILE=$(mktemp)
echo "$RECOMMENDED" > "$_MARK_TMPFILE"
python3 - "$_MARK_TMPFILE" "$DB_PATH" <<'EOF'
import sqlite3, json, sys, datetime
rows = json.loads(open(sys.argv[1]).read())
if not rows:
    sys.exit(0)
ids = [r[0] for r in rows]
now = datetime.datetime.utcnow().strftime('%Y-%m-%d %H:%M:%S')
conn = sqlite3.connect(sys.argv[2])
cur = conn.cursor()
cur.executemany("UPDATE jobs SET emailed_at = ? WHERE id = ?", [(now, jid) for jid in ids])
conn.commit()
conn.close()
print(f"Marked {len(ids)} job(s) as emailed at {now}.")
EOF
rm -f "$_MARK_TMPFILE"

echo "[jobseeker] ============================================"
echo "[jobseeker] Pipeline done! ${COUNT} job(s) processed."
echo "[jobseeker] Finished at $(date '+%Y-%m-%d %H:%M:%S')"
echo "[jobseeker] ============================================"
