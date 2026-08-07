// Package mailhdr turns raw mail header values into display text. It sits
// between the journal-milter, which reads headers off the wire, and the panel,
// which shows them: both need the same answer for the same header, and the
// panel needs it for rows the milter wrote before this decoding existed.
package mailhdr

import (
	"mime"
	"strings"
)

// SubjectMaxRunes caps what the journal keeps of a subject. A Subject header
// may legally run to hundreds of characters; the log only needs enough to
// recognise the message, and the panel shows one row per recipient.
const SubjectMaxRunes = 200

// DecodeSubject turns a raw Subject header into display text. Anything
// non-ASCII arrives as RFC 2047 encoded-words (=?utf-8?Q?=D0=9F…?=), which the
// panel would otherwise show verbatim: unreadable, and — being one unbreakable
// run — wide enough to push the send-log table out of its card. Go's decoder
// covers the UTF-8 and ASCII charsets senders use in practice; for anything
// else (windows-1251, koi8-r) it fails and the raw header is kept, which is no
// worse than not decoding at all. Truncation is applied after decoding so the
// cap counts characters of the subject, not bytes of its encoding.
//
// It is idempotent: already-decoded text contains no encoded-words, so a second
// pass returns it unchanged. That is what lets the panel decode on the way out
// as well as the milter on the way in.
func DecodeSubject(v string) string {
	if dec, err := (&mime.WordDecoder{}).DecodeHeader(v); err == nil {
		v = dec
	}
	v = strings.TrimSpace(v)
	if r := []rune(v); len(r) > SubjectMaxRunes {
		v = string(r[:SubjectMaxRunes]) + "…"
	}
	return v
}
