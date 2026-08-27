package worker

import "syscall"

func detachedAttributes() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
