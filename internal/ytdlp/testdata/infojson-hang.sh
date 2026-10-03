#!/bin/sh
# Test-Fake: wie infojson.sh (Mediendatei, "<Stamm>.info.json" und
# after_move:filepath-Zeile aus $PRINT_LINE), hängt danach, bis der Runner
# ihn per Context-Abbruch beendet.
mkdir -p "$(dirname "$PRINT_LINE")"
printf 'media' > "$PRINT_LINE"
printf '{"id":"abc","title":"Titel"}' > "${PRINT_LINE%.*}.info.json"
printf '%s\n' "$PRINT_LINE"
exec sleep 30
