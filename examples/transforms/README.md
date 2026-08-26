# Transforms

Turning a messy record into a tidy one — which is most of what a `transform`
block is for, and the part of Mycel you will write most often.

The payload a form or a partner sends is rarely the shape you want to store:
an address in whatever case it was typed, a name that may be missing, tags
that arrive as a list or as a single string, a date in one format and needed
in another.

## Files

| File | Purpose |
|------|---------|
| `flows.mycel` | One flow that normalises a contact, one that works on lists |
| `connectors.mycel` | REST and SQLite |
| `migrations/001_contacts.sql` | The table the tidy record goes into |

## Running

```bash
mycel migrate --config ./examples/transforms
mycel start --config ./examples/transforms
```

## Try It

A complete record:

```bash
curl -X POST http://localhost:3000/contacts \
  -H 'Content-Type: application/json' \
  -d '{"email":"  Ada.Lovelace@Example.COM ","name":"Ada Lovelace","nickname":"Ada","tags":["vip","beta"],"signed_up":"2026-03-14T09:00:00Z"}'
```

And a sparse one — no name, one tag sent as a bare string, no date:

```bash
curl -X POST http://localhost:3000/contacts \
  -H 'Content-Type: application/json' \
  -d '{"email":"BOB@corp.io","tags":"lead"}'
```

Both are stored in the same shape:

```bash
curl http://localhost:3000/contacts
```

```json
[
  { "display": "Ada",    "domain": "example.com", "email": "ada.lovelace@example.com",
    "initials": "A", "tags": "vip,beta", "tag_count": 2, "signed_up": "2026-03-14" },
  { "display": "friend", "domain": "corp.io",     "email": "bob@corp.io",
    "initials": "F", "tags": "lead",     "tag_count": 1, "signed_up": "2026-08-26" }
]
```

The second record had no name, so `display` fell back twice — nickname, then
name, then `"friend"`. Its one tag arrived as a string and came out as a list
of one. It sent no date, so it got today's.

## Lists of lists

```bash
curl -X POST http://localhost:3000/tags/flatten \
  -H 'Content-Type: application/json' \
  -d '{"groups": [["a","b"], ["c"], ["a"]]}'
```

```json
{ "flat": ["a","b","c","a"], "newest": ["a","c","b","a"], "distinct": ["a","b","c"], "total": 4 }
```

## What each one does

| Expression | What it is for |
|------------|----------------|
| `lower(trim(x))` | Compare addresses without caring how they were typed |
| `split(s, sep)[1]` | Cut a string and take a piece — here the domain after the `@` |
| `default(a, b)` | Fall back when a field is missing **or empty**, which is what a form sends. `coalesce` is the same function |
| `substring(s, 0, 1)` | Bytes from one index to another |
| `as_list(x)` | Accept either shape: a list stays a list, one value becomes a list of one, nothing becomes the empty list |
| `join(list, sep)` | Write a list back out as one string |
| `size(list)` / `len(string)` | Two questions, two functions — `len` on a list is a compile error rather than a wrong number |
| `format_date(d, fmt)` | Reformat an ISO date. Tokens: `YYYY MM DD HH mm ss` |
| `hash_sha256(s)` | A stable fingerprint: the same input always gives the same 64 characters |
| `now()` / `now_unix()` | A timestamp to read, and seconds to compare and sort |
| `flatten(list)` | One level of nesting removed |
| `reverse(list)` / `unique(list)` | Order and duplicates |

The full list is in the [CEL function reference](../../docs/reference/cel-functions.md).

## Notes

**`hash_sha256` is a fingerprint, not a password hash.** One unsalted pass of
SHA-256 is fast by design, which is exactly what someone holding your table
wants. Passwords belong to the [auth system](../../docs/guides/auth.md), which
uses Argon2id.

**`output` means two different things**, and the flow uses both. Inside the
`transform` block it is what has been computed above that line. Inside the
`response` block it is what the destination gave back — so `output.id` there is
the row the database wrote, not the `id` the transform built. The
[input and output page](../../docs/core-concepts/input-and-output.md) sets out
the whole table.
