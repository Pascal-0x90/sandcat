// +build darwin

package shells

import (
	"os/exec"
	"time"

	"github.com/mitre/gocat/execute"
)

type Osascript struct {
	shortName string
	path string
	execArgs []string
}

func init() {
	shell := &Osascript{
		shortName: "osa",
		path: "osascript",
		execArgs: []string{"-e"},
	}
	if shell.CheckIfAvailable() {
		execute.Executors[shell.shortName] = shell
	}
}

func (o *Osascript) Run(command string, timeout int, info execute.InstructionInfo) ([]byte, string, string, time.Time) {
	return runShellExecutor(*exec.Command(o.path, append(o.execArgs, command)...), timeout)
}

func (o *Osascript) String() string {
	return o.shortName
}

func (o *Osascript) CheckIfAvailable() bool {
	return checkExecutorInPath(o.path)
}

func (o *Osascript) DownloadPayloadToMemory(payloadName string) bool {
	return false
}

func (o *Osascript) UpdateBinary(newBinary string) {
	o.path = newBinary
}

func (o *Osascript) UpdateExecArgs(newArgs []string) {
	o.execArgs = make([]string, len(newArgs))
	copy(o.execArgs, newArgs)
}