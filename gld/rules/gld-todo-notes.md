<!-- Managed by gld: overwritten at every gld startup. Do not edit; add your own rules under another name. -->
# Todos in gld notes

When the user asks to note something "en todo" / as a todo, store it in gld's notepad file `${CLAUDE_CONFIG_DIR:-~/.claude}/gld-web-notes.json` (a JSON map `key -> text`).

- If the current project has a `todo.md` (repo root, any case), write nothing: ask with `AskUserQuestion` whether to add it to `todo.md` or to the gld notepad.
- Otherwise the key is `p/<project name>` when the user names a project, else `d/<absolute working directory>`.
- The text of a note is a JSON array of strings, serialized as a string (one item per todo). Append to the existing items; never touch other keys.
- Write atomically (temp file, then `mv`), mode `0600`, with `jq`:

```bash
f="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/gld-web-notes.json"
[ -f "$f" ] || echo '{}' > "$f"
jq --arg k "d/$PWD" --arg i "todo text" \
  '.[$k] = ((((.[$k] // "[]") | (fromjson? // (split("\n") | map(select(test("\\S")))))) + [$i]) | tojson)' \
  "$f" > "$f.tmp" && chmod 600 "$f.tmp" && mv "$f.tmp" "$f"
```

- If the file is not valid JSON, do not overwrite it; tell the user.
- Limits: 64 KB per note, 500 notes. Confirm in one line with the key and the item added.
