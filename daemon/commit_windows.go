package daemon

import (
	winlx "github.com/Microsoft/go-winlx"
	"github.com/docker/docker/layer"
)

func (daemon *Daemon) encodeOS(osVersion string, id layer.ChainID) layer.ChainID {
	if daemon.GraphDriverName() == "windowsfilter" && osVersion != "" {
		return layer.ChainID(winlx.EncodeOS(osVersion, id.String()))
	}
	return id
}
