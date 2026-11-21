#!/bin/bash
# run_weekly.sh <person>: run the weekly jobseeker pipeline for ONE person from that person's own data folder.
#
#   bash C:/workspace/jobseeker-tools/run_weekly.sh james
#
# This is the only command the Agent Hub job-hunter agent may run. It is installed OUTSIDE every folder the agent can
# write to, and changed only by the owner or a developer. The source of truth is tools/run_weekly.sh in the jobseeker
# repository; copy it to the tools folder after review.
#
# Rules:
#   - exactly one argument: the person's name, lower-case letters only
#   - the data folder <data root>/<person> must already exist (this script never creates a person)
#   - config and database are always that person's own files, passed explicitly by run_jobseeker.sh
#   - the full output (which includes the shortlist) goes to <person folder>/runs/<time>.log and nowhere else;
#     the terminal gets only a receipt with counts, never a job title or link
#   - sending email is off unless the owner's scheduler sets SEND_EMAIL, EMAIL_SCRIPT and TO in the environment
#   - delivery is a dry run (nothing marked as sent) unless the owner has created the marker file
#     <tools folder>/delivery/<person>.on . With the marker the run writes runs/digest-<time>.md in the person's folder
#     and marks those listings as sent; the agent then relays the digest as the weekly task says. The agent cannot create
#     the marker: the tools folder is outside every folder it can write to.
#
# Overrides, for tests and for the owner only (the agent cannot set environment variables):
#   JOBSEEKER_DATA_ROOT (default C:/workspace/jobseeker-data), JOBSEEKER_CODE (default C:/workspace/jobseeker),
#   JOBSEEKER_TOOLS (default: the folder this script is in)

set -uo pipefail

DATA_ROOT="${JOBSEEKER_DATA_ROOT:-C:/workspace/jobseeker-data}"
CODE="${JOBSEEKER_CODE:-C:/workspace/jobseeker}"
TOOLS="${JOBSEEKER_TOOLS:-$(cd "$(dirname "$0")" && pwd)}"

fail() { echo "[run_weekly] ERROR: $*" >&2; exit 2; }

[ "$#" -eq 1 ] || fail "usage: run_weekly.sh <person> (exactly one argument)"
PERSON="$1"
[[ "$PERSON" =~ ^[a-z]{1,32}$ ]] || fail "the person name must be 1 to 32 lower-case letters"
HOME_DIR="${DATA_ROOT}/${PERSON}"
[ -d "$HOME_DIR" ] || fail "no data folder for '${PERSON}' (it is created by the owner, not by this script)"
[ -f "$CODE/run_jobseeker.sh" ] || fail "run_jobseeker.sh not found in ${CODE}"
[ -f "$CODE/jobseeker.exe" ] || fail "jobseeker.exe not found in ${CODE} (build it: go build -o jobseeker.exe ./cmd/jobseeker)"

mkdir -p "$HOME_DIR/runs"
STAMP="$(date +%Y%m%d-%H%M%S)"
LOG="$HOME_DIR/runs/${STAMP}.log"

# One run at a time per person. A run takes tens of minutes; a second start would score the same listings twice (double cost) and
# fight over the database. The lock is a folder holding the owner's process id and start time. It is released when this script
# ends, and a lock whose process is gone, or that is older than four hours, is taken over.
LOCK="$HOME_DIR/runs/.lock"
if ! mkdir "$LOCK" 2>/dev/null; then
  OWNER_PID="$(cat "$LOCK/pid" 2>/dev/null || true)"
  if { [ -n "$OWNER_PID" ] && ! kill -0 "$OWNER_PID" 2>/dev/null; } || [ -n "$(find "$LOCK" -maxdepth 0 -mmin +240 2>/dev/null)" ]; then
    rm -rf "$LOCK"
    mkdir "$LOCK" 2>/dev/null || fail "could not take the run lock for '${PERSON}'"
  else
    echo "[run_weekly] a run for '${PERSON}' is already active (started $(cat "$LOCK/started" 2>/dev/null || echo unknown)). Do not start another: wait for it, then read its log in the runs folder. Nothing was started." >&2
    exit 3
  fi
fi
echo "$$" > "$LOCK/pid"
date '+%Y-%m-%d %H:%M:%S' > "$LOCK/started"
trap 'rm -rf "$LOCK"' EXIT

DELIVERY=none
[ -f "$TOOLS/delivery/${PERSON}.on" ] && DELIVERY=digest

START="$(date '+%Y-%m-%d %H:%M:%S')"
JOBSEEKER_HOME="$HOME_DIR" JOBSEEKER_BIN="$CODE/jobseeker.exe" DELIVERY="$DELIVERY" JOBSEEKER_RUN_STAMP="$STAMP"   bash "$CODE/run_jobseeker.sh" >"$LOG" 2>&1
RC=$?

echo "[run_weekly] person: ${PERSON}"
echo "[run_weekly] started: ${START}   exit code: ${RC}"
echo "[run_weekly] log file: runs/${STAMP}.log (in the person folder)"
echo "[run_weekly] delivery: $([ "$DELIVERY" = digest ] && echo "digest (listings are marked as sent when the digest is written)" || echo "off (dry run, nothing is marked as sent)")"
# Status lines only: they carry counts and step names, never a job title or link.
grep -E '^\[jobseeker\] (Found|No new|SEND_EMAIL|Digest written|Email sent|Marking|Marked|Step|ERROR)' "$LOG" || true
[ "$RC" -eq 0 ] || echo "[run_weekly] the run failed; read the log file for the reason" >&2
exit "$RC"
