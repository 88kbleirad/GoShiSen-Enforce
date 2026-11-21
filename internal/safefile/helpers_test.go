package safefile

import (
	"os/exec"
	"time"
)

func execCmd(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func timeAfter() <-chan time.Time {
	return time.After(3 * time.Second)
}
