// Package truncindex provides a general 'index tree', used by Docker
// in order to be able to reference containers by only a few unambiguous
// characters of their id.
package truncindex

// TruncIndex allows the retrieval of string identifiers by any of their unique prefixes.
// This is used to retrieve image and container IDs by more convenient shorthand prefixes.
type TruncIndex struct {
}

// NewTruncIndex creates a new TruncIndex and initializes with a list of IDs.
func NewTruncIndex(ids []string) (idx *TruncIndex) {
	return
}

func (idx *TruncIndex) addID(id string) error {
	return nil
}

// Add adds a new ID to the TruncIndex.
func (idx *TruncIndex) Add(id string) error {
	return nil
}

// Delete removes an ID from the TruncIndex. If there are multiple IDs
// with the given prefix, an error is thrown.
func (idx *TruncIndex) Delete(id string) error {
	return nil
}

// Get retrieves an ID from the TruncIndex. If there are multiple IDs
// with the given prefix, an error is thrown.
func (idx *TruncIndex) Get(s string) (string, error) {
	return "", nil
}
