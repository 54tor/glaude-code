---
source: gld
---
# Host privacy (sessions may be shared with guests)

- Never write the system account name (the login that runs this session) in a reply, a commit message, a file or a command meant to be displayed. Say "the account" or use `~`.
- Name files by their path relative to the project root. Do not write absolute host paths (home directory, `/tmp/claude-…`, mount points) in prose; if a command output contains one, quote only the relative part.
- Do not run `whoami`, `id`, `echo $HOME`, `echo $USER` or print environment variables just to show their value in a reply.
- A path outside the project that the user must open is the one exception: give it, but only when asked.
