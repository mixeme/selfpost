package milter

// The address OpenDKIM signs for, found as OpenDKIM finds it: a port of
// libopendkim's dkim_mail_parse() and its tokenizer, from Debian's
// opendkim 2.11.0~beta2-9.1+b1 (libopendkim/dkim-mailparse.c, identical
// there to upstream's develop branch), line for line. OpenDKIM calls it on the
// value of the message's first From: field (opendkim/opendkim.c, mlfi_eoh(),
// dkimf_findheader(dfc, "from", 0): build/opendkim.conf sets no SenderHeaders),
// lower-cases the domain it returns, and signs with the key whose SigningTable
// pattern matches "user@domain" (dkimf_apply_signtable()). Whatever the port
// returns, OpenDKIM does: quoted strings and domain literals are kept with
// their delimiters, comments are dropped, a source route resets the address,
// an '@' before the first ':', ';' or ',' decides where the address starts,
// and encoded words are not decoded.
//
// The C works in place on a NUL-terminated buffer, writing the result over
// the input; here the input is read through at(), which gives 0 at its end as
// the C finds the terminator, and the result goes to a buffer of its own, in
// which 0 ends a string as '\0' does there.

// mailparse errors, DKIM_MAILPARSE_ERR_* (dkim-mailparse.c).
const (
	mailparseOK          = 0
	mailparsePUnbalanced = 1 // unbalanced parentheses
	mailparseQUnbalanced = 2 // unbalanced quotes
	mailparseSUnbalanced = 3 // unbalanced square brackets
)

// mailparseSpecials is SPECIALS: "<>@,;:\\\"/[]?=".
var mailparseSpecials = func() (t [256]bool) {
	for _, c := range []byte("<>@,;:\\\"/[]?=") {
		t[c] = true
	}
	return t
}()

// mailparseSpace is isascii(c) && isspace(c) in the C locale.
func mailparseSpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

type mailparser struct {
	b []byte // the input up to its first NUL, as strlen() sees it
}

// at is *p: the byte, or the terminator at and beyond the end.
func (m *mailparser) at(i int) byte {
	if i >= len(m.b) {
		return 0
	}
	return m.b[i]
}

// matchingParen is dkim_mail_matching_paren(): the position of the close
// character that balances the open one before s, or e. An open character of
// 0 is never met inside the string, so for quotes and brackets it only skips
// to the close, past backslash escapes.
func (m *mailparser) matchingParen(s, e int, open, close byte) int {
	paren := 1
	for ; s < e; s++ {
		c := m.b[s]
		if c == close {
			paren--
			if paren == 0 {
				break
			}
		} else if c == open {
			paren++
		} else if c == '\\' {
			if m.at(s+1) != 0 {
				s++
			}
		}
	}
	return s
}

// firstSpecial is dkim_mail_first_special(): the '<' that opens an address,
// or the ':', ';' or ',' that ends a part - or the '@' seen before it - or
// the end.
func (m *mailparser) firstSpecial(p, e int) (int, int) {
	atPtr := -1
	for ; p < e && m.at(p) != 0; p++ {
		// skip white space between tokens
		for p < e && (m.at(p) == '(' || mailparseSpace(m.at(p))) {
			if m.at(p) != '(' {
				p++
			} else {
				p = m.matchingParen(p+1, e, '(', ')')
				if m.at(p) == 0 {
					return p, mailparsePUnbalanced
				}
				p++
			}
		}
		if m.at(p) == 0 {
			break
		}
		c := m.at(p)
		switch {
		case c == '"':
			p = m.matchingParen(p+1, e, 0, '"')
			if m.at(p) == 0 {
				return p, mailparseQUnbalanced
			}
		case c == '[':
			p = m.matchingParen(p+1, e, 0, ']')
			if m.at(p) == 0 {
				return p, mailparseSUnbalanced
			}
		case mailparseSpecials[c]:
			if c == '<' {
				return p, mailparseOK
			} else if c == ':' || c == ';' || c == ',' {
				if atPtr >= 0 {
					return atPtr, mailparseOK
				}
				return p, mailparseOK
			} else if c == '@' {
				atPtr = p
			}
		default:
			for m.at(p) != 0 && !mailparseSpecials[m.at(p)] && !mailparseSpace(m.at(p)) && m.at(p) != '(' {
				p++
			}
			p--
		}
	}
	return p, mailparseOK
}

// token is dkim_mail_token(): the next token from s - a quoted string or a
// domain literal with its delimiters, a special character alone, or an atom
// ('x') - its type (0 at the end), and whether whitespace outside a comment
// came before it (ws is set, never cleared, as in the C).
func (m *mailparser) token(s, e int, ws *bool) (typ int, start, end int, err int) {
	p := s
	// skip white space between tokens
	for p < e && (m.at(p) == '(' || mailparseSpace(m.at(p))) {
		if m.at(p) != '(' {
			*ws = true
			p++
		} else {
			p = m.matchingParen(p+1, e, '(', ')')
			if m.at(p) == 0 {
				return 0, 0, 0, mailparsePUnbalanced
			}
			p++
		}
	}
	if p >= e || m.at(p) == 0 {
		return 0, 0, 0, mailparseOK
	}
	start = p
	c := m.at(p)
	switch {
	case c == '"':
		end = m.matchingParen(p+1, e, 0, '"')
		typ = '"'
		if m.at(end) != 0 {
			end++
		} else {
			err = mailparseQUnbalanced
		}
	case c == '[':
		end = m.matchingParen(p+1, e, 0, ']')
		typ = '['
		if m.at(end) != 0 {
			end++
		} else {
			err = mailparseSUnbalanced
		}
	case mailparseSpecials[c]:
		end = p + 1
		typ = int(c)
	default:
		for p < e && m.at(p) != 0 && !mailparseSpecials[m.at(p)] && !mailparseSpace(m.at(p)) && m.at(p) != '(' {
			p++
		}
		end = p
		typ = 'x'
	}
	return typ, start, end, err
}

// mailParsed is what dkim_mail_parse() returns: the local part and the
// domain, each absent (NULL) or a string, and the error. rest is not the
// C's: where in the line it stopped reading, for the caller's question
// whether anything follows the address.
type mailParsed struct {
	user, domain       string
	hasUser, hasDomain bool
	err                int
	rest               int
}

// dkimMailParse is dkim_mail_parse().
func dkimMailParse(line string) mailParsed {
	if i := indexByte(line, 0); i >= 0 {
		line = line[:i] // strlen()
	}
	m := &mailparser{b: []byte(line)}
	e := len(m.b)
	var out []byte
	user, domain := -1, -1
	result := func(err, rest int) mailParsed {
		r := mailParsed{err: err, rest: rest}
		if user >= 0 {
			r.user, r.hasUser = cstring(out[user:]), true
		}
		if domain >= 0 {
			r.domain, r.hasDomain = cstring(out[domain:]), true
		}
		return r
	}
	ws := false
	pos := 0
	for {
		special, err := m.firstSpecial(pos, e)
		if err != 0 {
			return result(err, special)
		}
		switch m.at(special) {
		case '<':
			// display name <address>
			pos = special + 1
			for {
				typ, ts, te, err := m.token(pos, e, &ws)
				if err != 0 {
					return result(err, pos)
				}
				if typ == '>' || typ == 0 {
					out = append(out, 0)
					if typ == 0 {
						te = e
					}
					return result(mailparseOK, te)
				} else if typ == '@' {
					out = append(out, 0)
					domain = len(out)
				} else if typ == ',' || typ == ':' {
					// source route punctuation
					user, domain = -1, -1
				} else {
					if user < 0 {
						user = len(out)
					}
					out = append(out, m.b[ts:te]...)
				}
				pos = te
			}
		case ';', ':', ',':
			// skip a group name or result
			pos = special + 1
		default:
			// (display name) addr(display name)ess
			ws = false
			for {
				typ, ts, te, err := m.token(pos, e, &ws)
				if err != 0 {
					return result(err, pos)
				}
				if typ == 0 || typ == ',' || typ == ';' {
					out = append(out, 0)
					if typ == 0 {
						te = e
					}
					return result(mailparseOK, te)
				} else if typ == '@' {
					out = append(out, 0)
					domain = len(out)
					ws = false
				} else {
					if user < 0 {
						user = len(out)
					} else if typ == 'x' && ws {
						out = append(out, ' ')
					}
					out = append(out, m.b[ts:te]...)
					ws = false
				}
				pos = te
			}
		}
	}
}

// moreAfter says whether anything but list and group punctuation follows
// position rest of line: another address, or text after the one found.
func moreAfter(line string, rest int) bool {
	if i := indexByte(line, 0); i >= 0 {
		line = line[:i]
	}
	m := &mailparser{b: []byte(line)}
	ws := false
	for pos := rest; ; {
		typ, _, te, err := m.token(pos, len(m.b), &ws)
		if err != 0 {
			return true
		}
		switch typ {
		case 0:
			return false
		case ',', ';':
			pos = te
		default:
			return true
		}
	}
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// cstring reads a NUL-terminated string from b.
func cstring(b []byte) string {
	if i := indexByte(string(b), 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
