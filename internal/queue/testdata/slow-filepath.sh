#!/bin/sh
# Test-Fake für TestQueueAttributesFilenamesToCorrectConcurrentJob: leitet
# Zielverzeichnis (aus dem ERSTEN -o-Argument — dem Video-Output-Template;
# der Runner hängt danach noch ein zweites -o "thumbnail:..." fürs Poster an,
# das hier ignoriert wird, genau wie im echten yt-dlp der erste unpräfixierte
# -o das Standard-Template setzt) und Dateiname (aus dem letzten Argument,
# der Job-URL) ab, statt Umgebungsvariablen zu nutzen — so bleiben zwei
# gleichzeitig laufende Prozesse unabhängig voneinander. Job "a" pausiert
# länger als Job "b", damit b garantiert fertig ist, während a noch läuft
# (deckt Job-übergreifendes Überschreiben eines geteilten Callback-Felds auf).
outdir=""
prev=""
url=""
for arg in "$@"; do
	if [ "$prev" = "-o" ] && [ -z "$outdir" ]; then
		outdir=$(dirname "$arg")
	fi
	prev="$arg"
	url="$arg"
done

case "$url" in
*/a)
	name="youtube/KanalA/A [a].mkv"
	delay=0.4
	;;
*/b)
	name="youtube/KanalB/B [b].mkv"
	delay=0.05
	;;
*)
	name="unknown.mkv"
	delay=0
	;;
esac

printf 'dl:   1.2%%|500.00KiB/s|01:40\n'
sleep "$delay"
printf '%s/%s\n' "$outdir" "$name"
printf 'dl: 100.0%%|1.10MiB/s|00:00\n'
exit 0
