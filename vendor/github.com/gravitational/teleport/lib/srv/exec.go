/*
Copyright 2015 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package srv

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/shell"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/gravitational/trace"
	"github.com/kardianos/osext"
	log "github.com/sirupsen/logrus"
)

const (
	defaultPath          = "/bin:/usr/bin:/usr/local/bin:/sbin"
	defaultEnvPath       = "PATH=" + defaultPath
	defaultTerm          = "xterm"
	defaultLoginDefsPath = "/etc/login.defs"
)

// execResult is used internally to send the result of a command execution from
// a goroutine to SSH request handler and back to the calling client
type execResult struct {
	command string

	// returned exec code
	code int
}

type execReq struct {
	Command string
}

// execResponse prepares the response to a 'exec' SSH request, i.e. executing
// a command after making an SSH connection and delivering the result back.
type execResponse struct {
	cmdName string
	cmd     *exec.Cmd
	ctx     *ctx
}

// parseExecRequest parses SSH exec request
func parseExecRequest(req *ssh.Request, ctx *ctx) (*execResponse, error) {
	var e execReq
	if err := ssh.Unmarshal(req.Payload, &e); err != nil {
		return nil, trace.BadParameter("failed to parse exec request, error: %v", err)
	}

	// split up command by space to grab the first word
	args := strings.Split(e.Command, " ")

	if len(args) > 0 {
		_, f := filepath.Split(args[0])

		// is this scp request?
		if f == "scp" {
			// for 'scp' requests, we'll launch ourselves with scp parameters:
			teleportBin, err := osext.Executable()
			if err != nil {
				return nil, trace.Wrap(err)
			}
			e.Command = fmt.Sprintf("%s scp --remote-addr=%s --local-addr=%s %v",
				teleportBin,
				ctx.conn.RemoteAddr().String(),
				ctx.conn.LocalAddr().String(),
				strings.Join(args[1:], " "))
		}
	}
	ctx.exec = &execResponse{
		ctx:     ctx,
		cmdName: e.Command,
	}
	return ctx.exec, nil
}

func (e *execResponse) String() string {
	return fmt.Sprintf("Exec(cmd=%v)", e.cmdName)
}

// prepInteractiveCommand configures exec.Cmd object for launching an interactive command
// (or a shell)
func prepInteractiveCommand(ctx *ctx) (*exec.Cmd, error) {
	var (
		err      error
		runShell bool
	)
	// determine shell for the given OS user:
	if ctx.exec.cmdName == "" {
		runShell = true
		ctx.exec.cmdName, err = shell.GetLoginShell(ctx.login)
		if err != nil {
			log.Error(err)
			return nil, trace.Wrap(err)
		}
		// in test mode short-circuit to /bin/sh
		if ctx.isTestStub {
			ctx.exec.cmdName = "/bin/sh"
		}
	}
	c, err := prepareCommand(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// this configures shell to run in 'login' mode. from openssh source:
	// "If we have no command, execute the shell.  In this case, the shell
	// name to be passed in argv[0] is preceded by '-' to indicate that
	// this is a login shell."
	// https://github.com/openssh/openssh-portable/blob/master/session.c
	if runShell {
		c.Args = []string{"-" + filepath.Base(ctx.exec.cmdName)}
	}
	return c, nil
}

// prepareCommand configures exec.Cmd for executing a given command within an SSH
// session.
//
// 'cmd' is the string passed as parameter to 'ssh' command, like "ls -l /"
//
// If 'cmd' does not have any spaces in it, it gets executed directly, otherwise
// it is passed to user's shell for interpretation
func prepareCommand(ctx *ctx) (*exec.Cmd, error) {
	osUserName := ctx.login
	// configure UID & GID of the requested OS user:
	osUser, err := user.Lookup(osUserName)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	uid, err := strconv.Atoi(osUser.Uid)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	gid, err := strconv.Atoi(osUser.Gid)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// get user's shell:
	shell, err := shell.GetLoginShell(ctx.login)
	if err != nil {
		log.Warn(err)
	}
	if ctx.isTestStub {
		shell = "/bin/sh"
	}

	// try and get the public address from the first available proxy. if public_address
	// is not set, fall back to the hostname of the first proxy we get back.
	proxyHost := "<proxyhost>:3080"
	if ctx.srv != nil {
		proxies, err := ctx.srv.authService.GetProxies()
		if err != nil {
			log.Errorf("Unexpected response from authService.GetProxies(): %v", err)
		}

		if len(proxies) > 0 {
			proxyHost = proxies[0].GetPublicAddr()
			if proxyHost == "" {
				proxyHost = fmt.Sprintf("%v:%v", proxies[0].GetHostname(), defaults.HTTPListenPort)
				log.Debugf("public_address not set for proxy, returning proxyHost: %q", proxyHost)
			}
		}
	}

	// by default, execute command using user's shell like openssh does:
	// https://github.com/openssh/openssh-portable/blob/master/session.c
	c := exec.Command(shell, "-c", ctx.exec.cmdName)

	clusterName, err := ctx.srv.authService.GetDomainName()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	c.Env = []string{
		"LANG=en_US.UTF-8",
		getDefaultEnvPath(osUser.Uid, defaultLoginDefsPath),
		"HOME=" + osUser.HomeDir,
		"USER=" + osUserName,
		"SHELL=" + shell,
		teleport.SSHTeleportUser + "=" + ctx.teleportUser,
		teleport.SSHSessionWebproxyAddr + "=" + proxyHost,
		teleport.SSHTeleportHostUUID + "=" + ctx.srv.ID(),
		teleport.SSHTeleportClusterName + "=" + clusterName,
	}
	c.Dir = osUser.HomeDir
	c.SysProcAttr = &syscall.SysProcAttr{}
	if _, found := ctx.env["TERM"]; !found {
		c.Env = append(c.Env, "TERM="+defaultTerm)
	}

	// execute the command under requested user's UID:GID
	me, err := user.Current()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if me.Uid != osUser.Uid || me.Gid != osUser.Gid {
		userGroups, err := osUser.GroupIds()
		if err != nil {
			return nil, trace.Wrap(err)
		}
		groups := make([]uint32, 0)
		for _, sgid := range userGroups {
			igid, err := strconv.Atoi(sgid)
			if err != nil {
				log.Warnf("Cannot interpret user group: '%v'", sgid)
			} else {
				groups = append(groups, uint32(igid))
			}
		}
		if len(groups) == 0 {
			groups = append(groups, uint32(gid))
		}
		c.SysProcAttr.Credential = &syscall.Credential{
			Uid:    uint32(uid),
			Gid:    uint32(gid),
			Groups: groups,
		}
		c.SysProcAttr.Setsid = true
	}

	// apply environment variables passed from the client
	for n, v := range ctx.env {
		c.Env = append(c.Env, fmt.Sprintf("%s=%s", n, v))
	}
	// apply SSH_xx environment variables
	remoteHost, remotePort, err := net.SplitHostPort(ctx.conn.RemoteAddr().String())
	if err != nil {
		log.Warn(err)
	} else {
		localHost, localPort, err := net.SplitHostPort(ctx.conn.LocalAddr().String())
		if err != nil {
			log.Warn(err)
		} else {
			c.Env = append(c.Env,
				fmt.Sprintf("SSH_CLIENT=%s %s %s", remoteHost, remotePort, localPort),
				fmt.Sprintf("SSH_CONNECTION=%s %s %s %s", remoteHost, remotePort, localHost, localPort))
		}
	}
	if ctx.session != nil {
		if ctx.session.term != nil {
			c.Env = append(c.Env, fmt.Sprintf("SSH_TTY=%s", ctx.session.term.tty.Name()))
		}
		if ctx.session.id != "" {
			c.Env = append(c.Env, fmt.Sprintf("%s=%s", teleport.SSHSessionID, ctx.session.id))
		}
	}

	// if the server allows reading in of ~/.tsh/environment read it in
	// and pass environment variables along to new session
	if ctx.srv.PermitUserEnvironment() {
		filename := filepath.Join(osUser.HomeDir, ".tsh", "environment")
		userEnvs, err := utils.ReadEnvironmentFile(filename)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		c.Env = append(c.Env, userEnvs...)
	}
	return c, nil
}

// start launches the given command returns (nil, nil) if successful. execResult is only used
// to communicate an error while launching
func (e *execResponse) start(ch ssh.Channel) (*execResult, error) {
	var err error
	e.cmd, err = prepareCommand(e.ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	e.cmd.Stderr = ch.Stderr()
	e.cmd.Stdout = ch

	inputWriter, err := e.cmd.StdinPipe()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	go func() {
		io.Copy(inputWriter, ch)
		inputWriter.Close()
	}()

	if err := e.cmd.Start(); err != nil {
		e.ctx.Warningf("%v start failure err: %v", e, err)
		return e.collectStatus(e.cmd, trace.ConvertSystemError(err))
	}
	e.ctx.Infof("%v started", e)

	return nil, nil
}

func (e *execResponse) wait() (*execResult, error) {
	if e.cmd.Process == nil {
		e.ctx.Errorf("no process")
	}
	err := e.cmd.Wait()
	return e.collectStatus(e.cmd, err)
}

func (e *execResponse) collectStatus(cmd *exec.Cmd, err error) (*execResult, error) {
	status, err := collectStatus(e.cmd, err)
	// report the result of this exec event to the audit logger
	auditLog := e.ctx.srv.alog
	if auditLog == nil {
		return status, err
	}
	fields := events.EventFields{
		events.ExecEventCommand: strings.Join(cmd.Args, " "),
		events.EventUser:        e.ctx.teleportUser,
		events.EventLogin:       e.ctx.login,
		events.LocalAddr:        e.ctx.conn.LocalAddr().String(),
		events.RemoteAddr:       e.ctx.conn.RemoteAddr().String(),
		events.EventNamespace:   e.ctx.srv.getNamespace(),
	}
	if err != nil {
		fields[events.ExecEventError] = err.Error()
		if status != nil {
			fields[events.ExecEventCode] = strconv.Itoa(status.code)
		}
	}
	auditLog.EmitAuditEvent(events.ExecEvent, fields)
	return status, err
}

func collectStatus(cmd *exec.Cmd, err error) (*execResult, error) {
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			status := exitErr.Sys().(syscall.WaitStatus)
			return &execResult{code: status.ExitStatus(), command: cmd.Path}, nil
		}
		return nil, err
	}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		return nil, fmt.Errorf("unknown exit status: %T(%v)", cmd.ProcessState.Sys(), cmd.ProcessState.Sys())
	}
	return &execResult{code: status.ExitStatus(), command: cmd.Path}, nil
}

// getDefaultEnvPath returns the default value of PATH environment variable for
// new logins (prior to shell) based on login.defs. Returns a strings which
// looks like "PATH=/usr/bin:/bin"
func getDefaultEnvPath(uid string, loginDefsPath string) string {
	envPath := defaultEnvPath
	envSuPath := defaultEnvPath

	// open file, if it doesn't exist return a default path and move on
	f, err := os.Open(loginDefsPath)
	if err != nil {
		log.Infof("Unable to open %q: %v: returning default path: %q", loginDefsPath, err, defaultEnvPath)
		return defaultEnvPath
	}
	defer f.Close()

	// read path to login.defs file /etc/login.defs line by line:
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// skip comments and empty lines:
		if line == "" || line[0] == '#' {
			continue
		}

		// look for a line that starts with ENV_SUPATH or ENV_PATH
		fields := strings.Fields(line)
		if len(fields) > 1 {
			if fields[0] == "ENV_PATH" {
				envPath = fields[1]
			}
			if fields[0] == "ENV_SUPATH" {
				envSuPath = fields[1]
			}
		}
	}

	// if any error occurs while reading the file, return the default value
	err = scanner.Err()
	if err != nil {
		log.Warnf("Unable to read %q: %v: returning default path: %q", loginDefsPath, err, defaultEnvPath)
		return defaultEnvPath
	}

	// if requesting path for uid 0 and no ENV_SUPATH is given, fallback to
	// ENV_PATH first, then the default path.
	if uid == "0" {
		if envSuPath == defaultEnvPath {
			return envPath
		}
		return envSuPath
	}
	return envPath
}
