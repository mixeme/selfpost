# Security policy

## Supported versions

SelfPost follows SemVer. Fixes are issued for the **latest minor release of the
1.x line** only; there is no backporting to earlier minors. Upgrade before
reporting if you are behind — the image tag is `ghcr.io/mixeme/selfpost:X.Y.Z`.

| Version | Supported |
|---|---|
| latest 1.x | yes |
| earlier 1.x | no — upgrade first |
| 0.x | no (pre-release) |

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's private vulnerability reporting:
the *Report a vulnerability* button under the repository's
[Security tab](https://github.com/mixeme/selfpost/security). If you cannot use
it, mail `public@mixeme.ru` instead.

Useful in a report: the image tag, the reverse proxy in front of the panel, the
steps to reproduce, and what an attacker gains. A relevant excerpt of
`mail.log` or the panel's system log helps; strip recipient addresses first.

**No response time is promised.** SelfPost is maintained by one person, and a
deadline that cannot be honoured is worse than none. Reports are read and
answered as soon as the maintainer is able; a fix ships in a patch release,
with the timeline agreed in the thread.

Disclosure is coordinated by request, not by demand: please hold public details
until a patch is out. If you get no reply, that is not a request for a
continued embargo — disclose at your own discretion. Reporters are credited in
the CHANGELOG unless they ask not to be.

## In scope

The relay's job is to accept authenticated mail from an application and hand it
to the internet as the operator's domain, and nothing else. Breaking that is in
scope:

- **Open relay** — mail accepted from an unauthenticated sender, or relayed for
  a domain the sending application is not bound to
- **SASL bypass** — sending without valid credentials, or credential recovery
  from anything the container exposes
- **Cross-domain access** — an application or a panel session reaching a domain
  it was not granted
- **Secret disclosure** — DKIM private keys, the admin password hash, session
  tokens, or backup encryption material leaking to an unauthorised party
- **Panel authentication and session flaws** — login bypass, session fixation,
  CSRF on state-changing routes, privilege escalation
- **Rate-limit bypass** — evading either the Postfix-level backstop or the
  per-domain and per-application limits
- **Container escape** or privilege escalation from the panel's unprivileged
  user to root

## Out of scope

These are the operator's responsibility or accepted trade-offs, documented in
[docs/security.md](docs/security.md) and the
[operator guide](docs/guide.md):

- Host configuration the operator controls: a blocked port 25, a missing or
  wrong PTR record, DNS records not published, a self-signed or expired
  certificate on the reverse proxy
- Anything requiring the attacker to already have root on the host or write
  access to the `./data` bind mount
- Missing hardening headers or TLS options on the reverse proxy — SelfPost
  never terminates HTTPS itself
- Deliverability outcomes: mail rejected or filtered by a receiving provider is
  a policy decision of that provider, not a defect
- Denial of service through sheer volume against a single-tenant relay
- Vulnerabilities in upstream Postfix, OpenDKIM, or the base image — report
  those upstream; if SelfPost's configuration makes an upstream issue
  exploitable when it otherwise would not be, that *is* in scope

## Reports we cannot act on

Automated scanner output with no demonstrated impact, and reports whose only
content is a version number compared against a CVE list, are closed without
investigation.
