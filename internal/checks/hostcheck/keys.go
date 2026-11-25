package hostcheck

// Message keys used by this package. Every key must exist in
// internal/i18n/locales/en/hostcheck.json (checked by a test).
const (
	KeyDockerOK             = "docker.ok"               // uptime
	KeyDockerRestarting     = "docker.restarting"       // count
	KeyDockerOOM            = "docker.oom"              // (none)
	KeyDockerExited         = "docker.exited"           // code
	KeyDockerUnhealthy      = "docker.unhealthy"        // uptime
	KeyDockerRestartLoop    = "docker.restart_loop"     // count, max, uptime
	KeyDockerHealthStarting = "docker.health_starting"  // uptime
	KeyDockerPaused         = "docker.paused"           // (none)
	KeyDockerRequired       = "docker.required_missing" // name
	KeyDockerUnavailable    = "docker.unavailable"      // error

	KeyDiskOK         = "disk.ok"          // percent, free, mount, inodes
	KeyDiskSpaceWarn  = "disk.space_warn"  // percent, free, mount
	KeySpaceCrit      = "disk.space_crit"  // percent, free, mount
	KeyDiskInodesWarn = "disk.inodes_warn" // percent, free, mount
	KeyDiskInodesCrit = "disk.inodes_crit" // percent, free, mount
	KeyDiskUnreadable = "disk.unreadable"  // mount, error

	KeyBackupOK       = "backup.ok"        // age, size, file, max_age
	KeyBackupMissing  = "backup.missing"   // path
	KeyBackupStale    = "backup.stale"     // age, max_age, file, size
	KeyBackupEmpty    = "backup.empty"     // file, age, size
	KeyBackupTooSmall = "backup.too_small" // size, min_size, file, age
)

var allKeys = []string{
	KeyDockerOK, KeyDockerRestarting, KeyDockerOOM, KeyDockerExited, KeyDockerUnhealthy,
	KeyDockerRestartLoop, KeyDockerHealthStarting, KeyDockerPaused, KeyDockerRequired, KeyDockerUnavailable,
	KeyDiskOK, KeyDiskSpaceWarn, KeySpaceCrit, KeyDiskInodesWarn, KeyDiskInodesCrit, KeyDiskUnreadable,
	KeyBackupOK, KeyBackupMissing, KeyBackupStale, KeyBackupEmpty, KeyBackupTooSmall,
}
