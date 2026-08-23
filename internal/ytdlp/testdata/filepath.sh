#!/bin/sh
# Test-Fake: mischt Progress-Template-Zeilen mit einer after_move:filepath-
# Zeile (aus $PRINT_LINE), so wie yt-dlp es mit --print zusammen mit
# --progress-template tatsächlich ausgibt.
printf 'dl:   1.2%%|500.00KiB/s|01:40\n'
printf '%s\n' "$PRINT_LINE"
printf 'dl: 100.0%%|1.10MiB/s|00:00\n'
exit 0
