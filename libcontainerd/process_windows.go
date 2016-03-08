package libcontainerd

// process keeps the state for both main container process and exec process.
type process struct {
	client *client

	// id is the Container ID
	id string

	// This is a string identifier, not a PID (name is a little confusing IMO)
	processID string

	// On Windows, systemPid is the PID of the first process created in
	// a container, not the 'system' PID in the Linux context. In other words,
	// it's the PID returned by vmcompute.dll CreateProcessInComputeSystem()
	systemPid uint32
}
