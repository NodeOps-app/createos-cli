package oauth

import "os/exec"

func runCmd(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}
