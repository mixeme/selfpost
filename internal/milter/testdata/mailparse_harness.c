/*
 * Differential harness for internal/milter/mailparse.go: OpenDKIM's own
 * dkim_mail_parse() (libopendkim/dkim-mailparse.c of the opendkim source the
 * image runs, compiled in by #include), fed one hex-encoded line per input
 * line. Output per line: the error, then the local part and the domain, each
 * "-" when NULL and "=" followed by its bytes in hex otherwise.
 *
 *   test/upstream/mailparse-diff.sh <opendkim source dir>
 *
 * builds it in debian:trixie and runs TestMailParseMatchesOpenDKIM with it.
 */
#include <sys/types.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "dkim-mailparse.c"

static int hexval(int c)
{
	if (c >= '0' && c <= '9') return c - '0';
	if (c >= 'a' && c <= 'f') return c - 'a' + 10;
	return -1;
}

static void put(const unsigned char *s)
{
	if (s == NULL) {
		fputs(" -", stdout);
		return;
	}
	fputs(" =", stdout);
	for (; *s != '\0'; s++)
		printf("%02x", *s);
}

int main(void)
{
	static char hex[1 << 20];
	static unsigned char buf[1 << 19];

	while (fgets(hex, sizeof hex, stdin) != NULL) {
		size_t n = strcspn(hex, "\r\n"), i;
		unsigned char *user, *domain;
		int err;

		for (i = 0; i + 1 < n; i += 2)
			buf[i / 2] = (unsigned char) (hexval(hex[i]) << 4 | hexval(hex[i + 1]));
		buf[n / 2] = '\0';
		err = dkim_mail_parse(buf, &user, &domain);
		printf("%d", err);
		put(user);
		put(domain);
		putchar('\n');
	}
	return 0;
}
