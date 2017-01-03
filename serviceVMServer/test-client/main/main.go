package main

import "../winlx"
import "os"
import "fmt"

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s <tar_path> <layerfolder_path>\n", os.Args[0])
		return
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Printf("error opening tar file: %s\n", err.Error())
		return
	}
	defer file.Close()

	size, err := winlx.ServiceVMImportLayer(os.Args[2], file, winlx.Version2)
	if err != nil {
		fmt.Printf("couldn't import layer: %s\n", err.Error())
		return
	}
	fmt.Printf("Success! Got size: %d\n", size)
}

