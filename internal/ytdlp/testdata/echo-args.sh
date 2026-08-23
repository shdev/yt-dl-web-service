#!/bin/sh
# Test-Fake: schreibt die empfangenen Argumente zeilenweise in $ARGS_FILE,
# damit der Test die tatsächlich gebaute yt-dlp-Kommandozeile prüfen kann.
printf '%s\n' "$@" > "$ARGS_FILE"
exit 0
