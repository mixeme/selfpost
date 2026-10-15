#!/usr/bin/env python3
"""Supervisor event listener: bring the container down on unrecoverable failure.

If a managed program of the mail path exhausts its restart retries and
enters FATAL, or keeps crashing - CRASH_LIMIT unexpected exits within
CRASH_WINDOW seconds - this listener signals supervisord (its parent) to
terminate, so the whole container exits and Docker's `restart` policy starts it
again, rather than lingering "alive" with a dead Postfix/OpenDKIM/panel.
FATAL alone did not cover the second: a program reaches FATAL only through
exits before its startsecs, and one that crashed after it was restarted for
ever (audit 2026-10-12, L32; supervisor 4.2.5, supervisor/process.py,
Subprocess.finish()). logrotate is not on the mail path: its FATAL (a config
it would ignore) stays on Server > Health, and mail keeps flowing
(assumptions C-V5).

Communication with supervisord uses the event listener protocol over stdin and
stdout, so stdout must carry only protocol tokens.

supervisor 4.2.5, docs/events.rst, "Event Listener Notification Protocol": the
listener writes READY\\n, reads one header line of key:value tokens whose len
is the payload's length and eventname the event's type, reads the payload, and
answers "RESULT 2\\nOK". A PROCESS_STATE_FATAL payload is a token set,
"processname:cat groupname:cat from_state:BACKOFF"; a PROCESS_STATE_EXITED one
adds "expected:0" for an exit code the program's exitcodes do not list, "1"
otherwise (docs/events.rst; supervisor/events.py, ProcessStateEvent.payload()).
supervisord starts the listener as its own child, and on SIGTERM it shuts down
(docs/running.rst, "Signals"); it is PID 1 here (build/entrypoint.sh, exec).

CRASHEXIT_DRYRUN=1 writes "crashexit: would shut down" instead of signalling,
for the test (build/crashexit_test.go).
"""
import os
import signal
import sys
import time

# Programs whose failure does not take the container down
# (build/supervisord.conf): logrotate is not on the mail path, postfix-reload
# runs on demand - a reload it could not start is reported to whoever asked for
# it - syslog-relay carries OpenDKIM's log lines, not mail, and cert-reload
# looks for a renewed certificate once a day (a crash of it leaves the one
# Postfix serves).
SPARED = {"logrotate", "postfix-reload", "syslog-relay", "cert-reload"}

# CRASH_LIMIT unexpected exits of one program within CRASH_WINDOW seconds are a
# program that does not stay up. A program on the mail path restarts in
# seconds; five times in ten minutes is no passing fault.
CRASH_LIMIT = 5
CRASH_WINDOW = 600


def write_stdout(s):
    sys.stdout.write(s)
    sys.stdout.flush()


def say(s):
    sys.stderr.write("crashexit: " + s + "\n")
    sys.stderr.flush()


def shut_down():
    if os.environ.get("CRASHEXIT_DRYRUN") == "1":
        say("would shut down")
        return
    os.kill(os.getppid(), signal.SIGTERM)


def main():
    crashes = {}  # program -> times of its unexpected exits in the window
    while True:
        # Tell supervisord we are ready for the next event.
        write_stdout("READY\n")

        line = sys.stdin.readline()
        if not line:
            return
        headers = dict(pair.split(":", 1) for pair in line.split())
        payload_len = int(headers.get("len", 0))
        payload = sys.stdin.read(payload_len) if payload_len else ""
        tokens = dict(
            pair.split(":", 1) for pair in payload.split() if ":" in pair
        )
        name = tokens.get("processname", "")
        event = headers.get("eventname", "PROCESS_STATE_FATAL")

        if event == "PROCESS_STATE_EXITED":
            if name not in SPARED and tokens.get("expected") == "0":
                now = time.monotonic()
                times = [t for t in crashes.get(name, []) if now - t < CRASH_WINDOW]
                times.append(now)
                crashes[name] = times
                if len(times) >= CRASH_LIMIT:
                    say(
                        "%s exited unexpectedly %d times in %d seconds; shutting down container"
                        % (name, len(times), CRASH_WINDOW)
                    )
                    shut_down()
                    crashes[name] = []
        elif name in SPARED:
            say(
                "%s entered FATAL; it is not on the mail path, the container stays up"
                % name
            )
        else:
            say(
                "%s entered FATAL; shutting down container"
                % (name or "a managed process")
            )
            shut_down()

        write_stdout("RESULT 2\nOK")


if __name__ == "__main__":
    main()
