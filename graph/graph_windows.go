// +build windows

package graph

// setupInitLayer populates a directory with mountpoints suitable
// for bind-mounting dockerinit into the container. T
func SetupInitLayer(initLayer string) error {
	return nil
}

func (graph *Graph) restoreBaseImages() ([]string, error) {
	// TODO Windows. This needs implementing (@swernli)
	return nil, nil
}
