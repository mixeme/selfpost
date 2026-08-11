# Plan: narrow-page-alignment (heading and card do not share an edge)

**Status:** candidate  
**Version:** no bearing on semver — presentation only.  
**Order:** independent, small. Left over from
[visual-style](../roadmap.md) (closed in `5eaf665`).

---

## What it is

On **Settings** (`/account`) and the **user form** (`/users/new`, `/users/{uid}`)
the page's `h1` sits at the left of the reading measure while the card below it
floats to the right — they do not share a left edge, so the page reads as two
blocks that were laid out independently.

Measured on Settings at a 1240px viewport: the heading's box starts 12rem
(192px) left of the card's. The ink measures 187px, the remaining 5px being the
left side bearing of the "S".

## Why it happens

Two rules meet ([panel.css](../../internal/web/view/static/panel.css)):

```
main > *      { max-width: 48rem; margin-left: auto; margin-right: auto; }
.card.narrow  { max-width: 24rem; }
```

Every direct child of `main` is capped at the 48rem measure and centred in the
column. `.card.narrow` (specificity 0-2-0) overrides the `max-width` of
`main > *` (0-0-1) down to 24rem — but **not** the auto margins, which it does
not mention. So the card centres itself on the column at 24rem while the `h1`
centres itself on the same column at 48rem, and half the 24rem difference is
the 12rem offset between their left edges.

## What makes these two pages different

The panel has four templates with `class="card narrow"` — `login`, `setup`,
`account`, `user_form` — and eleven with a full-width `.card`. That gives three
groups, and only one of them has the problem:

| Group | Pages | Heading and card |
|---|---|---|
| Full-width card | `dashboard`, `status`, `backup`, `deliveries`, `delivery`, `domain_detail`, `domain_delete`, `mail_queue`, `system_log`, `users` | Both take the 48rem measure — same edge, nothing to notice |
| Narrow card, no navigation | `login`, `setup` | `main.page-login/.page-setup { max-width: 24rem }` narrows the whole column, so the `h1` is 24rem too — same edge |
| **Narrow card, with navigation** | **`account`, `user_form`** | **The column stays 64rem/48rem, so the two centre on different measures** |

So the distinguishing property is not Settings itself but a **combination**:
these are the only pages that are *both* signed-in (and therefore have a
navigation column beside them) *and* built from a single narrow card. Every
other page has one of those properties, never both.

## Why the obvious fix is not available

Copying what `login`/`setup` do — narrowing `main` — was tried in the restyle
and reverted in `aeba3f8`. `.shell` is a flex row that centres the navigation
column and the page **as a pair**, so narrowing the page slides the navigation
sideways: 296px between Settings and Domains, measured. The reason the same
rule is safe on `login`/`setup` is precisely that those pages have no
navigation column to be moved.

Two further attempts, also reverted: capping `main`'s children
(`main:has(> .card.narrow) > *`) moves the content block instead, and
left-aligning the card (`margin-left: 0`) moves it the other way. Anything that
changes the *column* is visible from the navigation; the fix has to change only
what happens **inside** the column.

## Directions to weigh

- Give the narrow card's `h1` (and footer) the same 24rem cap and the same auto
  margins, so heading, card and footer centre on one measure while the column
  keeps its width. Needs a hook — `main:has(> .card.narrow) > h1`, or a class
  the two templates set on their own heading.
- Or wrap heading and card in one 24rem block inside `main`, which makes the
  grouping explicit in the template rather than inferred by a selector.
- Or drop `.narrow` on these two pages and let their cards take the full
  measure, as every other signed-in page does. Cheapest, and worth pricing:
  the narrow card exists so a short form does not stretch its fields across
  48rem, which is a real reason on `login` but weaker on Settings, whose card
  is long.

## Done when

- On Settings and the user form the heading, the card and the footer line up on
  one left edge.
- The navigation column and the page's own width are byte-identical across
  every signed-in page — verified by rendering two pages at one viewport and
  comparing the column's edges, which is how the 296px regression was caught.
- `login` and `setup` are unchanged.

## Risks

- Low. The trap is the one already sprung twice: a rule that looks like it only
  affects one page but is read by the shell as a change of column width. Any
  candidate must be checked against a second page, not only the page being
  fixed.
