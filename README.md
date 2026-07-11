# SelfPost

Self-hosted outbound SMTP relay with a web control panel, shipped as a single
Docker image. Postfix + OpenDKIM + a small Go panel run together under
`supervisord`; the panel manages multiple sending domains, per-domain DKIM keys
and SASL-authenticated applications bound to their domain.

SelfPost sends mail straight to the internet from your own IP, with DKIM
signing, and is configured once through the panel. It is **outbound only** — it
does not receive mail, provide mailboxes, or offer webmail.

> **Status: under active development.** See [docs/specification.md](docs/specification.md)
> for the full requirements and [docs/implementation-plan.md](docs/implementation-plan.md)
> for the phased build plan.

## Requirements (site prerequisites)

SelfPost assumes the host already provides the conditions for sending from your
own IP — an unblocked outbound port 25, a static IP, configurable PTR/rDNS and a
reasonable IP reputation. Providing these is the operator's job, not a feature of
SelfPost. Detailed deployment docs land in a later phase.

## Repository

- Primary: <https://codeberg.org/mix/selfpost>
- Mirror: <https://github.com/mixeme/selfpost>

## License

[AGPL-3.0](LICENSE). The AGPL closes the "SaaS loophole": if you run a modified
version as a network-accessible service, you must make the modified source
available to its users — not only when you distribute copies of the code.
