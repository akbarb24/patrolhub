package service

import "time"

import "github.com/akbarbinaji/patrolhub/internal/config"

// testNow is a fixed "now" for tests: Wednesday 2026-10-07 23:00 WIB.
var testNow = time.Date(2026, 10, 7, 23, 0, 0, 0, config.Jakarta)
