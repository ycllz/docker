package main

import (
	"os/exec"
	"strings"

	"github.com/docker/docker/runconfig"
	"github.com/go-check/check"
)

// GH14530. Validates combinations of --net= with other options
func checkPS(out string) bool {
	return strings.Contains(out, "PID   USER")
}

func (s *DockerSuite) TestNetHostname(c *check.C) {

	var (
		out    string
		err    error
		runCmd *exec.Cmd
	)

	runCmd = exec.Command(dockerBinary, "run", "-h=name", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err != nil {
		c.Fatalf(out, err)
	}
	if !checkPS(out) {
		c.Fatalf("Expected PS output, got %s", out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=host", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err != nil {
		c.Fatalf(out, err)
	}
	if !checkPS(out) {
		c.Fatalf("Expected PS output, got %s", out)
	}

	runCmd = exec.Command(dockerBinary, "run", "-h=name", "--net=bridge", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err != nil {
		c.Fatalf(out, err)
	}
	if !checkPS(out) {
		c.Fatalf("Expected PS output, got %s", out)
	}

	runCmd = exec.Command(dockerBinary, "run", "-h=name", "--net=none", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err != nil {
		c.Fatalf(out, err)
	}
	if !checkPS(out) {
		c.Fatalf("Expected PS output, got %s", out)
	}

	runCmd = exec.Command(dockerBinary, "run", "-h=name", "--net=host", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkHostname.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkHostname, out)
	}

	runCmd = exec.Command(dockerBinary, "run", "-h=name", "--net=container:other", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkHostname.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkHostname, out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=container", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, "--net: invalid net mode: invalid container format container:<name|id>") {
		c.Fatalf("Expected error containing '%s' got %s", "--net: invalid net mode: invalid container format container:<name|id>", out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=weird", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, "invalid --net: weird") {
		c.Fatalf("Expected error containing '%s' got %s", "invalid --net: weird", out)
	}
}

func (s *DockerSuite) TestConflictContainerNetworkAndLinks(c *check.C) {
	var (
		out    string
		err    error
		runCmd *exec.Cmd
	)

	runCmd = exec.Command(dockerBinary, "run", "--net=container:other", "--link=zip:zap", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictContainerNetworkAndLinks.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictContainerNetworkAndLinks.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=host", "--link=zip:zap", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictHostNetworkAndLinks.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictHostNetworkAndLinks.Error(), out)
	}
}

func (s *DockerSuite) TestConflictNetworkModeAndOptions(c *check.C) {
	var (
		out    string
		err    error
		runCmd *exec.Cmd
	)

	runCmd = exec.Command(dockerBinary, "run", "--net=host", "--dns=8.8.8.8", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkAndDNS.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkAndDNS.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=container:other", "--dns=8.8.8.8", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkAndDNS.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkAndDNS.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=host", "--add-host=name:8.8.8.8", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkHosts.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkHosts.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=container:other", "--add-host=name:8.8.8.8", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkHosts.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkHosts.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=host", "--mac-address=92:d0:c6:0a:29:33", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictContainerNetworkAndMac.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictContainerNetworkAndMac.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=container:other", "--mac-address=92:d0:c6:0a:29:33", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictContainerNetworkAndMac.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictContainerNetworkAndMac.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=container:other", "-P", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkPublishPorts.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkPublishPorts.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=container:other", "-p", "8080", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkPublishPorts.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkPublishPorts.Error(), out)
	}

	runCmd = exec.Command(dockerBinary, "run", "--net=container:other", "--expose", "8000-9000", "busybox", "ps")
	if out, _, err = runCommandWithOutput(runCmd); err == nil {
		c.Fatalf(out, err)
	}
	if !strings.Contains(out, runconfig.ErrConflictNetworkExposePorts.Error()) {
		c.Fatalf("Expected error containing '%s' got %s", runconfig.ErrConflictNetworkExposePorts.Error(), out)
	}
}
