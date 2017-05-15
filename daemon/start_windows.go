package daemon

import (
	"fmt"
	"path/filepath"

	"github.com/docker/docker/container"
	"github.com/docker/docker/layer"
	"github.com/docker/docker/libcontainerd"
	"github.com/docker/docker/pkg/system"
)

const (
	credentialSpecRegistryLocation = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Virtualization\Containers\CredentialSpecs`
	credentialSpecFileLocation     = "CredentialSpecs"
)

func (daemon *Daemon) getLibcontainerdCreateOptions(container *container.Container) ([]libcontainerd.CreateOption, error) {
	createOptions := []libcontainerd.CreateOption{}

	// Are we going to run as a Hyper-V container?
	hvOpts := &libcontainerd.HyperVIsolationOption{}
	if container.HostConfig.Isolation.IsDefault() {
		// Container is set to use the default, so take the default from the daemon configuration
		hvOpts.IsHyperV = daemon.defaultIsolation.IsHyperV()
	} else {
		// Container is requesting an isolation mode. Honour it.
		hvOpts.IsHyperV = container.HostConfig.Isolation.IsHyperV()
	}

	dnsSearch := daemon.getDNSSearchSettings(container)
	if dnsSearch != nil {
		osv := system.GetOSVersion()
		if osv.Build < 14997 {
			return nil, fmt.Errorf("dns-search option is not supported on the current platform")
		}
	}

	// Generate the layer folder of the layer options
	layerOpts := &libcontainerd.LayerOption{}
	m, err := container.RWLayer.Metadata()
	if err != nil {
		return nil, fmt.Errorf("failed to get layer metadata - %s", err)
	}
	if hvOpts.IsHyperV {
		hvOpts.SandboxPath = filepath.Dir(m["dir"])
	}

	layerOpts.LayerFolderPath = m["dir"]

	// Generate the layer paths of the layer options
	img, err := daemon.imageStore.Get(container.ImageID)
	if err != nil {
		return nil, fmt.Errorf("failed to graph.Get on ImageID %s - %s", container.ImageID, err)
	}
	// Get the layer path for each layer.
	max := len(img.RootFS.DiffIDs)
	for i := 1; i <= max; i++ {
		img.RootFS.DiffIDs = img.RootFS.DiffIDs[:i]
		layerPath, err := layer.GetLayerPath(daemon.layerStore, img.RootFS.ChainID())
		if err != nil {
			return nil, fmt.Errorf("failed to get layer path from graphdriver %s for ImageID %s - %s", daemon.layerStore, img.RootFS.ChainID(), err)
		}
		// Reverse order, expecting parent most first
		layerOpts.LayerPaths = append([]string{layerPath}, layerOpts.LayerPaths...)
	}

	// Get endpoints for the libnetwork allocated networks to the container
	var epList []string
	AllowUnqualifiedDNSQuery := false
	gwHNSID := ""
	if container.NetworkSettings != nil {
		for n := range container.NetworkSettings.Networks {
			sn, err := daemon.FindNetwork(n)
			if err != nil {
				continue
			}

			ep, err := container.GetEndpointInNetwork(sn)
			if err != nil {
				continue
			}

			data, err := ep.DriverInfo()
			if err != nil {
				continue
			}

			if data["GW_INFO"] != nil {
				gwInfo := data["GW_INFO"].(map[string]interface{})
				if gwInfo["hnsid"] != nil {
					gwHNSID = gwInfo["hnsid"].(string)
				}
			}

			if data["hnsid"] != nil {
				epList = append(epList, data["hnsid"].(string))
			}

			if data["AllowUnqualifiedDNSQuery"] != nil {
				AllowUnqualifiedDNSQuery = true
			}
		}
	}

	if gwHNSID != "" {
		epList = append(epList, gwHNSID)
	}

	// Now add the remaining options.
	createOptions = append(createOptions, &libcontainerd.FlushOption{IgnoreFlushesDuringBoot: !container.HasBeenStartedBefore})
	createOptions = append(createOptions, hvOpts)
	createOptions = append(createOptions, layerOpts)

	var networkSharedContainerID string
	if container.HostConfig.NetworkMode.IsContainer() {
		networkSharedContainerID = container.NetworkSharedContainerID
	}
	createOptions = append(createOptions, &libcontainerd.NetworkEndpointsOption{
		Endpoints:                epList,
		AllowUnqualifiedDNSQuery: AllowUnqualifiedDNSQuery,
		DNSSearchList:            dnsSearch,
		NetworkSharedContainerID: networkSharedContainerID,
	})
	return createOptions, nil
}
