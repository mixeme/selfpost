#!/usr/bin/env python3
"""Supervisor event listener: bring the container down on unrecoverable failure.

If any managed program exhausts its restart retries and enters FATAL, this
listener signals supervisord (its parent) to terminate, so the whole container
exits and Docker's `restart` policy can recreate it cleanly — rather than
lingering "alive" with a dead Postfix/OpenDKIM/panel (spec 4).

Communication with supervisord uses the event listener protocol over stdin and
stdout, so stdout must carry only protocol tokens.
"""
import os
import signal
import sys


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
        if payload_len:
            sys.stdin.read(payload_len)

        # We only subscribe to PROCESS_STATE_FATAL, so any event means a managed
        # program can no longer be restarted. Take the container down.
        sys.stderr.write(
            "crashexit: a managed process entered FATAL; shutting down container\n"
        )
        sys.stderr.flush()
        os.kill(os.getppid(), signal.SIGTERM)

        write_stdout("RESULT 2\nOK")


if __name__ == "__main__":
    main()
