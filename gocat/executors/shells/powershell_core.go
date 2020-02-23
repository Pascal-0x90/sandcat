// +build windows darwin linux

package shells

import (
	"../execute"
	"os/exec"
	"runtime"
)

type PowershellCore struct {
	shortName string
	path string
	execArgs []string
}

func init() {
	var path string
	if runtime.GOOS == "windows" {
		path = "pwsh.exe"
	} else {
		path = "pwsh"
	}
	shell := &PowershellCore{
		shortName: "pwsh",
		path: path,
		execArgs: []string{"-C"},
	}
	if shell.CheckIfAvailable() {
		execute.Executors[shell.shortName] = shell
	}
}

func (p *PowershellCore) Run(command string, timeout int) ([]byte, string, string) {
	return runShellExecutor(*exec.Command(p.path, append(p.execArgs, command)...), timeout)
}

func (p *PowershellCore) String() string {
	return p.shortName
}

func (p *PowershellCore) CheckIfAvailable() bool {
	return checkExecutorInPath(p.path)
} 