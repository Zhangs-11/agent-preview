//go:build !darwin && !linux

package app

import "os/exec"

func configureDetached(command *exec.Cmd) {}
