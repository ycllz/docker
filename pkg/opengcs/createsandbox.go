package opengcs

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/Microsoft/hcsshim"
	"github.com/Sirupsen/logrus"
)

const defaultSandboxSize = 20 * 1024 // in MB

// Create a sandbox file. This is done by copying a prebuilt-sandbox from the ServiceVM
func CreateSandbox(uvm hcsshim.Container, filename string, maxSizeInMB uint32) error {
	if maxSizeInMB == 0 {
		maxSizeInMB = defaultSandboxSize
	}

	logrus.Debugf("opengcs: CreateSandbox: %s: %dMB", filename, maxSizeInMB)

	if uvm == nil {
		return fmt.Errorf("opengcs: CreateSandbox: No utility VM was supplied")
	}

	process, err := createUtilsProcess(uvm)
	if err != nil {
		return fmt.Errorf("opengcs: CreateSandbox: %s: failed to create utils process: %s", filename, err)
	}

	defer func() {
		//process.Process.Kill() TODO @jhowardmsft - This isn't currently implemented all the way through to GCS. Requires platform change.
		process.Process.Close()
	}()

	// Prepare payload data for CreateSandboxCmd command
	hdr := &protocolCommandHeader{
		Command:     cmdCreateSandbox,
		Version:     version1,
		PayloadSize: sandboxInfoHeaderSize,
	}

	hdrSandboxInfo := &sandboxInfoHeader{maxSandboxSizeInMB: maxSizeInMB}

	// Send protocolCommandHeader and SandboxInfoHeader to the Service VM

	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.BigEndian, hdr); err != nil {
		return fmt.Errorf("opengcs: CreateSandbox: %s: failed sending protocol header to utility VM: %s", filename, err)
	}
	if err := binary.Write(buf, binary.BigEndian, hdrSandboxInfo); err != nil {
		return fmt.Errorf("opengcs: CreateSandbox: %s: failed sending sandbox info header to utility VM: %s", filename, err)
	}

	logrus.Debugf("opengcs: CreateSandbox: %s: Writing %d bytes to utility VM", filename, buf.Bytes())
	if _, err = process.Stdin.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("opengcs: CreateSandbox: %s: %dMB: failed to send %d bytes to utility VM: %s", filename, maxSizeInMB, buf.Bytes(), err)
	}

	logrus.Debugf("opengcs: CreateSandbox: %s: waiting for utility VM to respond", filename)
	resultSize, err := waitForResponse(process.Stdout)
	if err != nil {
		return fmt.Errorf("opengcs: CreateSandbox: %s: failed waiting for a response from utility VM: %s", filename, err)
	}

	logrus.Debugf("opengcs: CreateSandbox: %s: writing %d bytes", filename, resultSize)
	// Get back the sandbox VHDx stream from the service VM and write it to file
	err = writeFileFromReader(filename, resultSize, process.Stdout)
	if err != nil {
		return fmt.Errorf("opengcs: CreateSandbox: %s: failed writing %d bytes to target file: %s", filename, resultSize, err)
	}

	logrus.Debugf("opengcs: CreateSandbox: %s created", filename)
	return nil
}
