package agent

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"code.google.com/p/go-shlex"
	"github.com/tutumcloud/tutum-agent/utils"
)

func DownloadDocker(url, dockerBinPath string) {
	if !utils.FileExist(dockerBinPath) {
		Logger.Println("Downloading docker binary...")
		downloadFile(url, dockerBinPath, "docker")
	}
	createDockerSymlink(dockerBinPath, DockerSymbolicLink)
}

func StartDocker(dockerBinPath, keyFilePath, certFilePath, caFilePath string) {
	var command *exec.Cmd
	var cmdstring string

	if *FlagDebugMode {
		cmdstring = fmt.Sprintf("-d -D -H %s -H %s --tlscert %s --tlskey %s --tlscacert %s --tlsverify",
			DockerDefaultHost, Conf.DockerHost, certFilePath, keyFilePath, caFilePath)
	} else {
		cmdstring = fmt.Sprintf("-d -H %s -H %s --tlscert %s --tlskey %s --tlscacert %s --tlsverify",
			DockerDefaultHost, Conf.DockerHost, certFilePath, keyFilePath, caFilePath)
	}
	if *FlagStandalone && !utils.FileExist(caFilePath) {
		if *FlagDebugMode {
			cmdstring = fmt.Sprintf("-d -D -H %s -H %s --tlscert %s --tlskey %s --tls",
				DockerDefaultHost, Conf.DockerHost, certFilePath, keyFilePath)
		} else {
			cmdstring = fmt.Sprintf("-d -H %s -H %s --tlscert %s --tlskey %s --tls",
				DockerDefaultHost, Conf.DockerHost, certFilePath, keyFilePath)
		}

		fmt.Fprintln(os.Stderr, "WARNING: standalone mode activated but no CA certificate found - client authentication disabled")
	}

	if Conf.DockerOpts != "" {
		cmdstring = cmdstring + " " + Conf.DockerOpts
	}

	cmdslice, err := shlex.Split(cmdstring)
	if err != nil {
		cmdslice = strings.Split(cmdstring, " ")
	}

	command = exec.Command(dockerBinPath, cmdslice...)

	go runDocker(command)
}

func StopDocker() {
	if DockerProcess != nil {
		DockerProcess.Signal(syscall.SIGTERM)
		for {
			if DockerProcess != nil {
				time.Sleep(500 * time.Millisecond)
			} else {
				break
			}
		}
	}
}

func UpdateDocker(dockerBinPath, dockerNewBinPath, dockerNewBinSigPath, keyFilePath, certFilePath, caFilePath string) {
	if utils.FileExist(dockerNewBinPath) {
		Logger.Printf("New Docker binary (%s) found", dockerNewBinPath)
		Logger.Println("Updating docker...")
		if verifyDockerSig(dockerNewBinPath, dockerNewBinSigPath) {
			Logger.Println("Stopping docker daemon")
			ScheduleToTerminateDocker = true
			StopDocker()
			Logger.Println("Removing old docker binary")
			if err := os.RemoveAll(dockerBinPath); err != nil {
				SendError(err, "Failed to remove the old docker binary", nil)
				Logger.Println("Cannot remove old docker binary:", err)
			}
			Logger.Println("Renaming new docker binary")
			if err := os.Rename(dockerNewBinPath, dockerBinPath); err != nil {
				SendError(err, "Failed to rename the docker binary", nil)
				Logger.Println("Cannot rename docker binary:", err)
			}
			Logger.Println("Removing the signature file", dockerNewBinSigPath)
			if err := os.RemoveAll(dockerNewBinSigPath); err != nil {
				SendError(err, "Failed to remove the docker sig file", nil)
				Logger.Println(err)
			}
			createDockerSymlink(dockerBinPath, DockerSymbolicLink)
			ScheduleToTerminateDocker = false
			StartDocker(dockerBinPath, keyFilePath, certFilePath, caFilePath)
			Logger.Println("Docker binary updated successfully")
		} else {
			Logger.Println("Cannot verify signature. Rejecting update")
			Logger.Println("Removing the invalid docker binary", dockerNewBinPath)
			if err := os.RemoveAll(dockerNewBinPath); err != nil {
				SendError(err, "Failed to remove the invalid docker binary", nil)
				Logger.Println(err)
			}
			Logger.Println("Removing the invalid signature file", dockerNewBinSigPath)
			if err := os.RemoveAll(dockerNewBinSigPath); err != nil {
				SendError(err, "Failed to remove the invalid docker sig file", nil)
				Logger.Println(err)
			}
			Logger.Println("Failed to update docker binary")
		}
	}
}

func verifyDockerSig(dockerNewBinPath, dockerNewBinSigPath string) bool {
	cmd := exec.Command("gpg", "--verify", dockerNewBinSigPath, dockerNewBinPath)
	err := cmd.Run()
	if err != nil {
		SendError(err, "GPG verification failed", nil)
		Logger.Println("GPG verification failed:", err)
		return false
	}
	Logger.Println("GPG verification passed")
	return true
}

func createDockerSymlink(dockerBinPath, dockerSymbolicLink string) {
	if err := os.RemoveAll(DockerSymbolicLink); err != nil {
		SendError(err, "Failed to remove the old docker symbolic link", nil)
		Logger.Println(err)
	}
	if err := os.Symlink(dockerBinPath, DockerSymbolicLink); err != nil {
		SendError(err, "Failed to create docker symbolic link", nil)
		Logger.Println(err)
	}
}

func runDocker(cmd *exec.Cmd) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		SendError(err, "Failed to get docker piped stdout", nil)
		Logger.Println(err)
		Logger.Println("Cannotget docker piped stdout")
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		SendError(err, "Failed to get docker piped stdout", nil)
		Logger.Println(err)
		Logger.Println("Cannotget docker piped stdout")
	}

	//open file to log docker logs
	dockerLog := path.Join(LogDir, DockerLogFileName)
	f, err := os.OpenFile(dockerLog, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		SendError(err, "Failed to set docker log file", nil)
		Logger.Println(err)
		Logger.Println("Cannot set docker log to", dockerLog)
	} else {
		go io.Copy(f, stdout)
		go io.Copy(f, stderr)
		defer f.Close()
	}

	Logger.Println("Starting docker daemon:", cmd.Args)
	if err := cmd.Start(); err != nil {
		SendError(err, "Failed to start docker daemon", nil)
		Logger.Println("Cannot start docker daemon:", err)
	}
	DockerProcess = cmd.Process
	Logger.Printf("Docker daemon (PID:%d) has been started", DockerProcess.Pid)

	syscall.Setpriority(syscall.PRIO_PROCESS, DockerProcess.Pid, RenicePriority)

	exit_renice := make(chan int)

	go decreaseDockerChildProcessPriority(exit_renice)

	if err := cmd.Wait(); err != nil {
		out, tailErr := exec.Command("tail", "-n", "50", dockerLog).Output()
		if tailErr != nil {
			SendError(tailErr, "Failed to tail docker logs when docker terminates unexpectedly", nil)
			Logger.Printf("Failed to tail docker logs when docker terminates unexpectedly: %s", err)
			SendError(err, "Docker daemon terminates unexpectedly", nil)
		} else {
			extra := map[string]interface{}{"docker-log": string(out)}
			SendError(err, "Docker daemon terminates unexpectedly", extra)
		}

		Logger.Println("Docker daemon died with error:", err)
	}
	exit_renice <- 1
	Logger.Println("Docker daemon died")
	DockerProcess = nil
}

func decreaseDockerChildProcessPriority(exit_renice chan int) {
	for {
		select {
		case <-exit_renice:
			return
		default:
			out, err := exec.Command("ps", "axo", "pid,ppid,ni").Output()
			if err != nil {
				SendError(err, "Failed to run ps command", nil)
				time.Sleep(ReniceSleepTime * time.Second)
				continue
			}
			lines := strings.Split(string(out), "\n")
			ppids := []int{DockerProcess.Pid}
			for _, line := range lines {
				items := strings.Fields(line)
				if len(items) != 3 {
					continue
				}
				pid, err := strconv.Atoi(items[0])
				if err != nil {
					continue
				}
				ppid, err := strconv.Atoi(items[1])
				if err != nil {
					continue
				}
				ni, err := strconv.Atoi(items[2])
				if err != nil {
					continue
				}
				if ni != RenicePriority {
					continue
				}
				if pid == DockerProcess.Pid {
					continue
				}
				for _, _ppid := range ppids {
					if ppid == _ppid {
						syscall.Setpriority(syscall.PRIO_PROCESS, pid, 0)
						ppids = append(ppids, pid)
						break
					}
				}
			}
			time.Sleep(ReniceSleepTime * time.Second)
		}
	}
}
