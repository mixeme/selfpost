# Plan: logrotate-mode (mail.log stops rotating in some images)

**Status:** candidate  
**Version:** patch; no schema, no configuration surface.  
**Order:** independent. Worth doing before anything that lets an instance run
unattended for months.

---

## What was observed

The container log carries, on every start:

```
warning: Potentially dangerous mode on /etc/logrotate.d/mail: 0664
error: Ignoring /etc/logrotate.d/mail because it is writable by group or others.
```

logrotate refuses a configuration file that group or others may write, so
`mail.log` is never rotated in an image with that mode. It grows until the
volume does.

Measured across three images built on the same host from the same commit range:

| Image built from | Mode of `/etc/logrotate.d/mail` |
|---|---|
| a sync made two days earlier | `0644` — works |
| a later sync (`tar -czf -` pipe from a Windows checkout) | `0666` |
| a later sync (`git archive` from the same checkout) | `0664` |

So the file is fine in the repository (git records `100644`) and is spoiled on
the way into the build context. `COPY build/logrotate-mail.conf
/etc/logrotate.d/mail` ([build/Dockerfile](../../build/Dockerfile)) takes the
mode from the context as it finds it, and an archive produced from a checkout
without POSIX permissions carries the umask-widened mode instead of the one git
recorded.

**Not established:** whether images built by the release workflow are affected.
They are built from a checkout on Linux, where the mode should survive as
`0644`, but the published image could not be pulled to check. Confirm before
concluding that only locally built images have this.

## Why it deserves a plan rather than a one-line fix

Three separate things are wrong, and fixing only the visible one leaves the
other two.

1. **The image trusts the build context's file modes.** Every `COPY` in the
   Dockerfile has this property, not just this one; the scripts happen to be
   `chmod +x`-ed afterwards, which is why they were never noticed.
2. **The failure is silent.** `logrotate-loop.sh` runs
   `logrotate /etc/logrotate.d/mail` and only reports a failure on a non-zero
   exit — but logrotate *ignores* the file and exits 0, so the loop reports
   nothing and the operator's only clue is a warning printed once at start.
3. **Nothing checks the outcome.** No test or health check notices that
   `mail.log` has not rotated, and the panel's Status page has no view of it.

## Directions to weigh

- `COPY --chmod=0644` on the configuration files (and an explicit mode on the
  scripts instead of the later `chmod +x`), which makes the image's file modes a
  property of the Dockerfile rather than of whoever built it. Needs a check of
  the minimum BuildKit version the project is willing to require.
- Or an explicit `chmod` in the same `RUN` that already fixes the scripts —
  cruder, no build-time requirement.
- Make `logrotate-loop.sh` fail loudly: `logrotate` has `--debug`-free ways to
  be told to care, but the simplest reliable check is that the loop verifies
  the configuration is readable-and-not-writable before entering the loop, and
  exits non-zero so supervisord reports it.
- Consider whether the e2e stack should assert that a rotation actually happens
  (it can run with a short `LOGROTATE_INTERVAL_SECONDS`).

## Done when

- An image built from a Windows checkout and one built by the release workflow
  both carry `0644`, and rotation runs in both.
- A configuration logrotate would ignore makes the container say so in a way an
  operator will see, rather than exiting 0.
- The dev loop's sync step cannot silently widen file modes again, or the image
  no longer cares if it does.

## Risks

- Low blast radius, but it touches the image's startup path — a mistake here is
  a container that will not start rather than a log that does not rotate.
- The `create 0640 postfix selfpost` line in the rotate configuration is load
  bearing (see the comment in `logrotate-loop.sh`: a postlogd-triggered recreate
  lands the file unreadable by the unprivileged panel). Any rework of the
  configuration must keep it.
