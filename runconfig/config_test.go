package runconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"github.com/docker/docker/pkg/stringutils"
)

type f struct {
	file       string
	entrypoint *stringutils.StrSlice
}

func TestDecodeContainerConfig(t *testing.T) {

	var (
		fixtures []f
		image    string
	)

	if runtime.GOOS != "windows" {
		image = "ubuntu"
		fixtures = []f{
			{"fixtures/unix/container_config_1_14.json", stringutils.NewStrSlice()},
			{"fixtures/unix/container_config_1_17.json", stringutils.NewStrSlice("bash")},
			{"fixtures/unix/container_config_1_19.json", stringutils.NewStrSlice("bash")},
		}
	} else {
		image = "windows"
		fixtures = []f{
			{"fixtures/windows/container_config_1_19.json", stringutils.NewStrSlice("cmd")},
		}
	}

	for _, f := range fixtures {
		b, err := ioutil.ReadFile(f.file)
		if err != nil {
			t.Fatal(err)
		}

		c, h, err := DecodeContainerConfig(bytes.NewReader(b))
		if err != nil {
			t.Fatal(fmt.Errorf("Error parsing %s: %v", f, err))
		}

		if c.Image != image {
			t.Fatalf("Expected %s image, found %s\n", image, c.Image)
		}

		if c.Entrypoint.Len() != f.entrypoint.Len() {
			t.Fatalf("Expected %v, found %v\n", f.entrypoint, c.Entrypoint)
		}

		if h != nil && h.Memory != 1000 {
			t.Fatalf("Expected memory to be 1000, found %d\n", h.Memory)
		}
	}
}

// check if (a == c && b == d) || (a == d && b == c)
// because maps are randomized
func compareRandomizedStrings(a, b, c, d string) error {
	if a == c && b == d {
		return nil
	}
	if a == d && b == c {
		return nil
	}
	return fmt.Errorf("strings don't match")
}

func TestDecodeContainerConfigVolumes(t *testing.T) {
	var (
		config     *Config
		hostConfig *HostConfig
		err        error
		tryit      []string
	)

	// A single volume
	tryit = choosePlatformVolumeArray([]string{`/tmp`}, []string{`c:\tmp`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
		t.Fatal(err)
	}
	if hostConfig.Binds != nil {
		t.Fatalf("Error parsing volume flags, %q should not mount-bind anything. Received %v", tryit, hostConfig.Binds)
	} else if _, exists := config.Volumes[tryit[0]]; !exists {
		t.Fatalf("Error parsing volume flags, %q is missing from volumes. Received %v", tryit, config.Volumes)
	}

	// Two volumes
	tryit = choosePlatformVolumeArray([]string{`/tmp`, `/var`}, []string{`c:\tmp`, `c:\var`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
		t.Fatal(err)
	}
	if hostConfig.Binds != nil {
		t.Fatalf("Error parsing volume flags, %q should not mount-bind anything. Received %v", tryit, hostConfig.Binds)
	} else if _, exists := config.Volumes[tryit[0]]; !exists {
		t.Fatalf("Error parsing volume flags, %s is missing from volumes. Received %v", tryit[0], config.Volumes)
	} else if _, exists := config.Volumes[tryit[1]]; !exists {
		t.Fatalf("Error parsing volume flags, %s is missing from volumes. Received %v", tryit[1], config.Volumes)
	}

	// A single bind-mount
	tryit = choosePlatformVolumeArray([]string{`/hostTmp:/containerTmp`}, []string{os.Getenv("TEMP") + `:c:\containerTmp`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
		t.Fatal(err)
	}
	if hostConfig.Binds == nil || hostConfig.Binds[0] != tryit[0] {
		t.Fatalf("Error parsing volume flags, %q should mount-bind the path before the colon into the path after the colon. Received %v", tryit[0], hostConfig.Binds)
	}

	// Two bind-mounts.
	tryit = choosePlatformVolumeArray([]string{`/hostTmp:/containerTmp`, `/hostVar:/containerVar`}, []string{os.Getenv("TEMP") + `:c:\containerTmp`, os.Getenv("ProgramFiles") + `:c:\ContainerPF`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
		t.Fatal(err)
	}
	if hostConfig.Binds == nil || compareRandomizedStrings(hostConfig.Binds[0], hostConfig.Binds[1], tryit[0], tryit[1]) != nil {
		t.Fatalf("Error parsing volume flags, `%s and %s` did not mount-bind correctly. Received %v", tryit[0], tryit[1], hostConfig.Binds)
	}

	// Two bind-mounts, first read-only, second read-write.
	// TODO Windows: The Windows version uses read-write as that's the only mode it supports. Can change this post TP4
	tryit = choosePlatformVolumeArray([]string{`/hostTmp:/containerTmp:ro`, `/hostVar:/containerVar:rw`}, []string{os.Getenv("TEMP") + `:c:\containerTmp:rw`, os.Getenv("ProgramFiles") + `:c:\ContainerPF:rw`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
		t.Fatal(err)
	}
	if hostConfig.Binds == nil || compareRandomizedStrings(hostConfig.Binds[0], hostConfig.Binds[1], tryit[0], tryit[1]) != nil {
		t.Fatalf("Error parsing volume flags, `%s and %s` did not mount-bind correctly. Received %v", tryit[0], tryit[1], hostConfig.Binds)
	}

	// Similar to previous test but with alternate modes which are only supported by Linux
	if runtime.GOOS != "windows" {
		tryit = choosePlatformVolumeArray([]string{`/hostTmp:/containerTmp:ro,Z`, `/hostVar:/containerVar:rw,Z`}, []string{})
		if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
			t.Fatal(err)
		}
		if hostConfig.Binds == nil || compareRandomizedStrings(hostConfig.Binds[0], hostConfig.Binds[1], tryit[0], tryit[1]) != nil {
			t.Fatalf("Error parsing volume flags, `%s and %s` did not mount-bind correctly. Received %v", tryit[0], tryit[1], hostConfig.Binds)
		}

		tryit = choosePlatformVolumeArray([]string{`/hostTmp:/containerTmp:Z`, `/hostVar:/containerVar:z`}, []string{})
		if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
			t.Fatal(err)
		}
		if hostConfig.Binds == nil || compareRandomizedStrings(hostConfig.Binds[0], hostConfig.Binds[1], tryit[0], tryit[1]) != nil {
			t.Fatalf("Error parsing volume flags, `%s and %s` did not mount-bind correctly. Received %v", tryit[0], tryit[1], hostConfig.Binds)
		}
	}

	// One bind mount and one volume
	tryit = choosePlatformVolumeArray([]string{`/hostTmp:/containerTmp`, `/containerVar`}, []string{os.Getenv("TEMP") + `:c:\containerTmp`, `c:\containerTmp`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
		t.Fatal(err)
	}
	if hostConfig.Binds == nil || len(hostConfig.Binds) > 1 || hostConfig.Binds[0] != tryit[0] {
		t.Fatalf("Error parsing volume flags, %s and %s should only one and only one bind mount %s. Received %s", tryit[0], tryit[1], tryit[0], hostConfig.Binds)
	} else if _, exists := config.Volumes[tryit[1]]; !exists {
		t.Fatalf("Error parsing volume flags %s and %s. %s is missing from volumes. Received %v", tryit[0], tryit[1], tryit[1], config.Volumes)
	}

	// Root to non-c: drive letter (Windows specific)
	if runtime.GOOS == "windows" {
		tryit = choosePlatformVolumeArray([]string{}, []string{os.Getenv("SystemDrive") + `\:d:`})
		if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
			t.Fatal(err)
		}
		if hostConfig.Binds == nil || len(hostConfig.Binds) > 1 || hostConfig.Binds[0] != tryit[0] || len(config.Volumes) != 0 {
			t.Fatalf("Error parsing %s. Should have a single bind mount and no volumes", tryit[0])
		}
	}

	// A single volume that looks like a bind mount passed in Volumes (not BCCLIVolumes), such as a REST API caller.
	// This should be handled as a bind mount, not a volume. (Linux specific)
	if runtime.GOOS != "windows" {
		tryit = choosePlatformVolumeArray([]string{`/foo:/bar`}, []string{})
		if config, hostConfig, err = callDecodeContainerConfig(tryit, false); err != nil {
			t.Fatal(err)
		}
		if hostConfig.Binds != nil {
			t.Fatalf("Error parsing volume flags, %q should not mount-bind anything. Received %v", tryit, hostConfig.Binds)
		} else if _, exists := config.Volumes[tryit[0]]; !exists {
			t.Fatalf("Error parsing volume flags, %q is missing from volumes. Received %v", tryit, config.Volumes)
		}
	}

	//
	// Error cases
	//

	// Empty spec
	tryit = []string{}
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err != nil {
		if hostConfig.Binds != nil || len(config.Volumes) != 0 {
			t.Fatalf("Empty volume spec should have no binds or volumes. Received %s %q", hostConfig.Binds, config.Volumes)
		}
	}

	// Root to root
	tryit = choosePlatformVolumeArray([]string{`/:/`}, []string{os.Getenv("SystemDrive") + `\:c:\`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
		t.Fatalf("%s should have failed", tryit[0])
	}

	// No destination path
	tryit = choosePlatformVolumeArray([]string{`/tmp:`}, []string{os.Getenv("TEMP") + `\:`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
		t.Fatalf("%s should have failed", tryit[0])
	}

	// No destination path or mode
	tryit = choosePlatformVolumeArray([]string{`/tmp::`}, []string{os.Getenv("TEMP") + `\::`})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
		t.Fatalf("%s should have failed", tryit[0])
	}

	// A whole lot of nothing
	tryit = []string{`:`}
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
		t.Fatalf("%s should have failed", tryit[0])
	}

	// A whole lot of nothing with no mode
	tryit = []string{`::`}
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
		t.Fatalf("%s should have failed", tryit[0])
	}

	// Too much including an invalid mode
	wTmp := os.Getenv("TEMP")
	tryit = choosePlatformVolumeArray([]string{`/tmp:/tmp:/tmp:/tmp:`}, []string{wTmp + ":" + wTmp + ":" + wTmp + ":" + wTmp})
	if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
		t.Fatalf("%s should have failed", tryit[0])
	}

	// Windows specific error tests
	if runtime.GOOS == "windows" {
		// Volume which does not include a drive letter
		tryit = []string{"\tmp"}
		if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
			t.Fatalf("%s should have failed", tryit[0])
		}

		// Root to C-Drive
		if runtime.GOOS == "windows" {
			tryit = choosePlatformVolumeArray([]string{}, []string{os.Getenv("SystemDrive") + `\:c:`})
			if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
				t.Fatalf("%s should have failed", tryit[0])
			}
		}

		// Container path that does not include a drive letter
		tryit = []string{"c:\tmp:\tmp"}
		if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
			t.Fatalf("%s should have failed", tryit[0])
		}
	}

	// Linux-specific error tests
	if runtime.GOOS != "windows" {
		// Just root
		tryit = []string{`/`}
		if config, hostConfig, err = callDecodeContainerConfig(tryit, true); err == nil {
			t.Fatalf("%s should have failed", tryit[0])
		}
	}
}

// choosePlatformVolumeArray takes two arrays of volume specs - a Unix style
// spec and a Windows style spec. Depending on the platform being unit tested,
// it returns one of the arrays.
func choosePlatformVolumeArray(u []string, w []string) []string {
	if runtime.GOOS == "windows" {
		return w
	}
	return u
}

// callDecodeContainerConfig is a utility function used by TestDecodeContainerConfigVolumes
// to call DecodeContainerConfig. It effectively does what a client would
// do when calling the daemon by constructing a JSON stream of a
// ContainerConfigWrapper which is populated by the set of volume specs
// passed into it. It returns a config and a hostconfig which can be
// validated to ensure DecodeContainerConfig has manipulated the structures
// correctly.
func callDecodeContainerConfig(volumes []string, inBCCLIVolumes bool) (*Config, *HostConfig, error) {
	var (
		b   []byte
		err error
		c   *Config
		h   *HostConfig
	)
	w := ContainerConfigWrapper{
		Config: &Config{
			BCCLIVolumes: map[string]struct{}{},
			Volumes:      map[string]struct{}{},
		},
		HostConfig: &HostConfig{NetworkMode: "none"},
	}
	for _, v := range volumes {
		if inBCCLIVolumes {
			w.Config.BCCLIVolumes[v] = struct{}{}
		} else {
			w.Config.Volumes[v] = struct{}{}
		}
	}
	if b, err = json.Marshal(w); err != nil {
		return nil, nil, fmt.Errorf("Error on marshal %s", err.Error())
	}
	c, h, err = DecodeContainerConfig(bytes.NewReader(b))
	if err != nil {
		return nil, nil, fmt.Errorf("Error parsing %s: %v", string(b), err)
	}
	if c == nil || h == nil {
		return nil, nil, fmt.Errorf("Empty config or hostconfig")
	}
	// There should be nothing in the backwards compatible field
	if c != nil && len(c.BCCLIVolumes) != 0 {
		return nil, nil, fmt.Errorf("BCCLIVolumes should be empty after calling DecodeContainerConfig")
	}

	return c, h, err
}
