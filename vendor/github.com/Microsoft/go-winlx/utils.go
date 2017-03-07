package winlx

import "strings"
import "fmt"

const osSplit = "-"

func EncodeOS(id, os string) string {
	return os + osSplit + id
}

func DecodeOS(id string) (string, string, error) {
	i := strings.Index(id, osSplit)
	if i == len(id)-1 || i <= 0 {
		// This should never happen
		return "", "", fmt.Errorf("Invalid encoded os format: %s", id)
	}
	return id[:i], id[i+1:], nil
}
