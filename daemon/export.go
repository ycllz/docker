package daemon

import (
	"io"
)

// ContainerExport writes the contents of the container to the given
// writer. An error is returned if the container cannot be found.
func (daemon *Daemon) ContainerExport(name string, out io.Writer) error {
	container, err := daemon.Get(name)
	if err != nil {
		return err
	}

	data, err := container.export()
	if err != nil {
		return nil
	}
	defer data.Close()

	// Stream the entire contents of the container (basically a volatile snapshot)
	if _, err := io.Copy(out, data); err != nil {
		return nil
	}
	return nil
}
