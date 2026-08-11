# Plan: logrotate-mode (mail.log stops rotating in some images)

**Status:** done  
**Version:** patch; no schema, no configuration surface.

---

## Summary

`mail.log` stopped rotating when `/etc/logrotate.d/mail` landed in the image
with group/other write permission (e.g. build context from a Windows tar sync).
logrotate ignores such configs but exits 0, so the loop looked healthy while
the log grew without bound.

## What shipped

- [`build/Dockerfile`](../../build/Dockerfile): `COPY --chmod` pins config
  (`0644`) and script (`0755`) modes so the image no longer depends on checkout
  file modes.
- [`build/logrotate-loop.sh`](../../build/logrotate-loop.sh): preflight and
  per-iteration checks refuse group/other-writable configs; logrotate stderr
  mentioning `Ignoring` is fatal.
- [`test/e2e/logrotate_check.go`](../../test/e2e/logrotate_check.go): asserts
  mode `644`, forced `logrotate -f`, and that a group-writable context file
  still produces `644` in the image.

`create 0640 postfix selfpost` in [`build/logrotate-mail.conf`](../../build/logrotate-mail.conf)
is unchanged — required for panel readability after rotation.
