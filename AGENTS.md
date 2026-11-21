# Agent policy for the Jobseeker project

This file is the policy every Agent Hub agent working on this project follows. It outranks any role brief.

## What the project is

Jobseeker scans job boards for one person, scores each listing against that person's resume, and reports the listings that fit.
The code is in this repository. Each person's data is in a separate folder outside it. Today there is one user, the owner, and
the project is a pilot: it is built so that other people can use it later.

## Roles

| Role | Does | Does not |
|---|---|---|
| `job-hunter` | Runs the weekly pipeline for the person named in the task, reads the result, reports counts, failures and fit | Edit this repository, apply for a job, contact anyone, write tailored CVs or cover letters, check recruiter job descriptions, read another person's folder |
| `reviewer` | Checks the facts of a job-hunter report (counts, scores, links) and of any change here | Approve its own work |
| Owner | Assigns the weekly task, merges changes, decides anything about people, accounts or credentials | |

A developer changes this repository only on a branch, through a pull request the owner merges.

## Data and privacy

- **One folder per person**, outside this repository: `C:/workspace/jobseeker-data/<person>/`. It holds the person's `.env`,
  `configs/config.yaml`, `resumes/`, `jobseeker.db` and `runs/`. Never read or write another person's folder.
- **Never commit** anything from a data folder, a resume, a database, an export, a key, an email address or a phone number.
  `.gitignore` covers the usual names; do not rely on it alone.
- **Private in every post, report, commit and log:** the resume, contact details, saved searches, listing history, the database and
  credentials. Say "the resume", not what is in it. The shortlist (titles, scores, reasons, links) stays in the person's folder
  and travels only by the route the owner approved. The task thread gets counts, failures and a receipt (file name, run time).

## Running the pipeline

- The only command a job-hunter runs is `bash C:/workspace/jobseeker-tools/run_weekly.sh <person>`. It takes exactly one argument,
  refuses anything else, and writes the full output to the person's `runs/` folder.
- Dry run first. Sending is off unless the owner's scheduler turns it on; a listing is marked as sent only after it was sent.
- A new person needs `jobseeker init` run once by the owner before the first run; the job-hunter does not run it. If the run stops with "Run 'jobseeker init' first", report that and stop.
- If the site blocks the scan, report it and skip. Do not work around a block, and respect each site's terms and robots.txt.
- The pipeline needs `MINIMAX_API_KEY` in the person's `.env`. Check that it is set, never print or copy it.
- The agent's work is search, analyse, report. The commands `checkjd`, `tailorcv` and cover letters are not part of it and need no
  Anthropic API key (`CLAUDE_API_KEY`); the owner may still run them by hand.

## Changing this repository

Branch, change, run `bash tests/run_jobseeker_test.sh` and `bash tests/run_weekly_test.sh` and `go build ./...`, open a pull request.
Do not rewrite history or force-push without the owner's say.
