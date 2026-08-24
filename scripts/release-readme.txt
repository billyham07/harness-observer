harness-observer {{VERSION}}

Local, read-only usage observability for coding-agent harnesses. It reads the
session files your agents already write on this machine, normalizes them, and
serves a dashboard on 127.0.0.1:9464.


WHAT IS IN THIS ARCHIVE

  {{BINARY}} - the program
  {{RUNTIME}} - DuckDB {{DUCKDB}} client library

Keep the two files in the same folder. The program links DuckDB dynamically and
will not start without it beside the executable (or on the library path).


RUNNING IT

  {{BINARY}}

On Windows this opens the dashboard in a native window (gpui). On Linux it
streams NDJSON (collect); use `{{BINARY}} web` or `{{BINARY}} gpui` there.

  {{BINARY}} web

Opens the same dashboard in a browser and keeps it current: the same process
rescans your session files on a timer, so there is no scheduled task to set up.
Add --no-open to skip launching a browser, --addr to bind somewhere else.

  {{BINARY}} --help        every command
  {{BINARY}} version       this build, the Go toolchain, the store schema
  {{BINARY}} doctor        which session roots were found, and what was read


WHAT IT DOES AND DOES NOT DO

Session files are opened read-only and are never rewritten, moved or deleted.
Nothing is proxied, no credentials are read, and no usage data leaves the
machine. The dashboard binds loopback by default because this is data about
one person's computer.

The one thing that touches the network is an optional public model price list,
downloaded to estimate an API-equivalent cost. It is a price list and nothing
else: no usage, no session content, no model names are sent. Disable it with
--pricing-refresh=false, and the bundled snapshot still works offline.

Cost figures are labelled "Est. API cost" because that is what they are: what
the observed tokens would have cost at published API rates. They are not your
subscription spend, and a model with no published rate is shown as unpriced
rather than as free.


WHERE THE DATA GOES

One DuckDB file in your per-user data directory. The dashboard prints the path
on startup and shows it in the left rail. Override it with --db, or the
HARNESS_OBSERVER_DB environment variable. Deleting it loses nothing permanent:
everything is rebuilt from the session files on the next scan.


https://github.com/billyham07/harness-observer
