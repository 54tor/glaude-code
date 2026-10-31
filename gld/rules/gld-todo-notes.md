---
source: gld
---
<!-- Managed by gld: overwritten at every gld startup. Do not edit; add your own rules under another name. -->
# Todos in gld notes

gld has a notepad per folder and per project, shown next to the sessions in its web UI. It is one JSON file, `${CLAUDE_CONFIG_DIR:-~/.claude}/gld-web-notes.json`: a map `key -> text`. Todos live there.

## When it applies

The user asks to note something as a todo: "note en todo", "ajoute aux todos", "à faire : …", "add a todo", "remind me to …" (as a to-do, not a time-based reminder). Several unrelated items in one message are several todos; related ones are merged into one (see Format). It also covers reading ("what are my todos?"), ticking off and removing items.

## Where to write (in this order)

1. The current project has a `todo.md` (repo root, any case): write nothing. Ask with `AskUserQuestion` whether to add the item to `todo.md` or to the gld notepad (2 options, recommend `todo.md`). Apply the answer to this request only.
2. The user names a project ("dans le projet X"): key `p/<X>`, exactly as named.
3. Otherwise: key `d/<absolute working directory>` (no trailing slash).

## Format

The value is a JSON array of strings, serialized as a string, one item per todo, e.g. `"[\"fix login\",\"bump deps\"]"`. This is what the UI reads and writes. A legacy value with one item per line is also read; rewrite it as an array when you touch it.

- Each todo is self-contained and independent of every other todo. The user later picks a few of them for a work session ("crunch"), in any order, possibly in a fresh session with no memory of this one. So a todo must be understandable and doable on its own: what to do, where (files, commands, URLs), why, and how to tell it is done. No "see above", no "after the previous one".
- Prefer one long todo over many small ones. Steps of the same task, or a task and its follow-ups, are one todo, not a list. Split only when the parts can be picked up separately; if one needs another, merge them.
- Wording: the user's own words and language, enriched with the context needed to meet the rule above, without inventing requirements. A todo may span several sentences. No checkbox syntax, no date, no numbering, no prefix.
- Append at the end. Skip an item that is already present (exact match) and say so.
- Never touch other keys. Never delete a note unless the user asked to clear it; an empty note is removed by the UI, so to clear one delete its key.

## How to write

Atomic (temp file, then `mv`), mode `0600`, with `jq`. Append:

```bash
f="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/gld-web-notes.json"
[ -f "$f" ] || echo '{}' > "$f"
jq --arg k "d/$PWD" --arg i "todo text" \
  '.[$k] = ((((.[$k] // "[]") | (fromjson? // (split("\n") | map(select(test("\\S")))))) + [$i]) | tojson)' \
  "$f" > "$f.tmp" && chmod 600 "$f.tmp" && mv "$f.tmp" "$f"
```

Read the items of a key: `jq -r --arg k "d/$PWD" '.[$k] // "[]" | fromjson? // empty | .[]' "$f"`.

Remove an item (exact text): same pipeline with `| map(select(. != $i))`; when nothing is left, `del(.[$k])` instead.

## Safety

- If the file exists but is not valid JSON, do not overwrite it: tell the user.
- If `jq` is missing, say so and stop; do not hand-edit the JSON.
- Limits: 64 KB per note, 500 notes. Above that, say so instead of truncating.
- gld holds a lock while saving from the UI and this write does not; run the command once, never in a loop.

## Reply

One line: the key used and the item added (or removed). For a read, list the items as bullets. Nothing else.
