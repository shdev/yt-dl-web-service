package ytdlp

import (
	"strconv"
	"strings"

	"ytdlweb/internal/job"
)

// ProgressTemplate erzeugt maschinenlesbare Fortschrittszeilen (Spec §4.4).
const ProgressTemplate = "dl:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s"

func ParseProgress(line string) (job.Progress, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "dl:")
	if !ok {
		return job.Progress{}, false
	}
	parts := strings.Split(rest, "|")
	if len(parts) != 3 {
		return job.Progress{}, false
	}
	pctStr := strings.TrimSuffix(strings.TrimSpace(parts[0]), "%")
	pct, err := strconv.ParseFloat(pctStr, 64)
	if err != nil {
		return job.Progress{}, false
	}
	speed := strings.TrimSpace(parts[1])
	eta := strings.TrimSpace(parts[2])
	if speed == "NA" {
		speed = ""
	}
	if eta == "NA" {
		eta = ""
	}
	return job.Progress{Percent: pct, Speed: speed, ETA: eta}, true
}
