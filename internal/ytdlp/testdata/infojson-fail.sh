#!/bin/sh
# Test-Fake: wie infojson.sh (Mediendatei, "<Stamm>.info.json" und
# after_move:filepath-Zeile aus $PRINT_LINE), aber der Download scheitert
# danach mit Exit-Code 1.
mkdir -p "$(dirname "$PRINT_LINE")"
printf 'media' > "$PRINT_LINE"
printf '{"id":"abc","title":"Titel"}' > "${PRINT_LINE%.*}.info.json"
printf '%s\n' "$PRINT_LINE"
echo "ERROR: HTTP Error 403: Forbidden" >&2
exit 1
