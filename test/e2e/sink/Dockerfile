# SMTP sink-MX for the e2e stand (plan C.4): accepts any mail on 25 and dumps
# each transaction to its own file under /mail, so the test harness can read
# the raw received message (headers + body, including the DKIM-Signature
# Postfix/OpenDKIM added) straight off disk. smtp-sink ships in the postfix
# package itself — no new dependency.
FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN echo "postfix postfix/mailname string localhost" | debconf-set-selections \
 && echo "postfix postfix/main_mailer_type string Internet Site" | debconf-set-selections \
 && apt-get update \
 && apt-get install -y --no-install-recommends postfix \
 && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /mail
WORKDIR /mail

# -d: one file per transaction (a pseudo-random suffix is appended
# automatically), so the harness just watches the directory for a new file
# instead of parsing a shared multi-message dump. Binding :25 needs root, and
# smtp-sink refuses to run as root without an explicit -u to switch to after
# binding — "-u root" satisfies that check while staying root throughout,
# which is fine here: throwaway container, isolated e2e network, never
# exposed.
ENTRYPOINT ["/usr/sbin/smtp-sink"]
CMD ["-u", "root", "-d", "/mail/%Y%m%d%H%M%S.", "0.0.0.0:25", "100"]
