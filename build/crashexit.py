#!/usr/bin/env python3
"""Supervisor event listener: bring the container down on unrecoverable failure.

If a managed program of the mail path exhausts its restart retries and
enters FATAL, this listener signals supervisord (its parent) to terminate, so
the whole container exits and Docker's `restart` policy starts it again —
rather than lingering "alive" with a dead Postfix/OpenDKIM/panel. logrotate is
not on the mail path: its FATAL (a config it would ignore) stays on
Server > Health, and mail keeps flowing (assumptions C-V5).

Communication with supervisord uses the event listener protocol over stdin and
stdout, so stdout must carry only protocol tokens.

supervisor 4.2.5, docs/events.rst, "Event Listener Notification Protocol": the
listener writes READY\\n, reads one header line of key:value tokens whose len
is the payload's length, reads the payload, and answers "RESULT 2\\nOK".
A PROCESS_STATE_FATAL payload is a token set, "processname:cat groupname:cat
from_state:BACKOFF" (docs/events.rst; supervisor/events.py,
ProcessStateEvent.payload()).
supervisord starts the listener as its own child, and on SIGTERM it shuts down
(docs/running.rst, "Signals"); it is PID 1 here (build/entrypoint.sh, exec).
"""
import os
import signal
import sys

# Programs whose FATAL does not take the container down (build/supervisord.conf).
SPARED = {"logrotate"}


def write_stdout(s):
    sys.stdout.write(s)
    sys.stdout.flush()


def main():
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

        # We only subscribe to PROCESS_STATE_FATAL, so any event means a managed
        # program can no longer be restarted.
        if name in SPARED:
            sys.stderr.write(
                "crashexit: %s entered FATAL; it is not on the mail path, the "
                "container stays up\n" % name
            )
            sys.stderr.flush()
        else:
            sys.stderr.write(
                "crashexit: %s entered FATAL; shutting down container\n"
                % (name or "a managed process")
            )
            sys.stderr.flush()
            os.kill(os.getppid(), signal.SIGTERM)

        write_stdout("RESULT 2\nOK")


if __name__ == "__main__":
    main()
