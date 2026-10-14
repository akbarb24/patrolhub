package domain

// NightStatusLabel is the Indonesian name of a night status.
func NightStatusLabel(s NightStatus) string {
	switch s {
	case NightOpen:
		return "Terbuka"
	case NightReported:
		return "Dilaporkan"
	case NightNoReport:
		return "Tanpa laporan"
	case NightRainRequested:
		return "Menunggu konfirmasi hujan"
	case NightRainedOut:
		return "Hujan"
	}
	return string(s)
}

// AttendanceLabel is the Indonesian name of an attendance status.
func AttendanceLabel(s AttendanceStatus) string {
	switch s {
	case Present:
		return "Hadir"
	case Absent:
		return "Tidak hadir"
	case RainedOut:
		return "Hujan"
	}
	return string(s)
}
