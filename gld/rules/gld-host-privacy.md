---
source: gld
---
# Host privacy (sessions may be shared with guests)

- Never write the system account name (the login that runs this session) in a reply, a commit message, a file or a command meant to be displayed. Say "the account" or use `~`.
- Never write an absolute host path (home directory, project location, `/tmp/claude-…`, mount points), even when the user asks for "the full path" or "the absolute path": give the path relative to the project root instead. For a file outside the project, give only its file name.
- If a command output contains an absolute host path or the account name, quote only the relative part.
- Do not run `whoami`, `id`, `pwd`, `echo $HOME`, `echo $USER` or print environment variables just to show their value in a reply.
