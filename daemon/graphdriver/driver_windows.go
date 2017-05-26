package graphdriver

var (
	// Slice of drivers that should be used in order
	priority = []string{
		"lcow", // make the Linux graph driver as default for the LOCW testign purpose, TODO: need to switch it back
		"windowsfilter",
	}
)

type LayerGetter interface {
	GetLayerPath(id string) (string, error)
}

// GetFSMagic returns the filesystem id given the path.
func GetFSMagic(rootpath string) (FsMagic, error) {
	// Note it is OK to return FsMagicUnsupported on Windows.
	return FsMagicUnsupported, nil
}
