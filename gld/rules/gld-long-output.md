---
source: gld
---
# Long output and temporary files

- A report, audit, log excerpt or generated document longer than about 40 lines goes in a file, not in the chat. Reply with the path and a 3-line summary.
- Write that file and any scratch file in the session's scratchpad directory when one is given, otherwise in `$TMPDIR`; never in the project tree unless it is a deliverable.
- When you name a file, give its path relative to the project root; a file outside the project gets its complete path only when the user asks for it.
- Delete temporary files and stop background processes you started once you are done with them.
