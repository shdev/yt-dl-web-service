#!/bin/sh
# Test-Fake: legt wie yt-dlp mit --write-info-json die Mediendatei und
# "<Stamm>.info.json" an (Pfad aus $PRINT_LINE) und gibt die
# after_move:filepath-Zeile aus. Mit gesetztem $NO_INFOJSON entsteht keine
# info.json.
mkdir -p "$(dirname "$PRINT_LINE")"
printf 'media' > "$PRINT_LINE"
if [ -z "$NO_INFOJSON" ]; then
  printf '{"id":"abc","title":"Titel"}' > "${PRINT_LINE%.*}.info.json"
fi
printf 'dl: 100.0%%|1.10MiB/s|00:00\n'
printf '%s\n' "$PRINT_LINE"
exit 0
