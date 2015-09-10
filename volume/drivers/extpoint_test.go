package volumedrivers

import (
	"runtime"
	"testing"

	"github.com/docker/docker/volume"
	"github.com/docker/docker/volume/testutils"
)

func TestGetDriver(t *testing.T) {
	_, err := GetDriver("missing")
	if err == nil {
		t.Fatal("Expected error, was nil")
	}
	Register(volumetestutils.FakeDriver{}, "fake")
	d, err := GetDriver("fake")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name() != "fake" {
		t.Fatalf("Expected fake driver, got %s\n", d.Name())
	}
}

// testParseMountSpec is a structure used by TestParseMountSpecSplit for
// specifying test cases for the ParseMountSpec() function.
type testParseMountSpec struct {
	bind      string
	driver    string
	expDest   string
	expSource string
	expName   string
	expDriver string
	expRW     bool
	fail      bool
}

func TestParseMountSpecSplit(t *testing.T) {
	var cases []testParseMountSpec
	if runtime.GOOS == "windows" {
		cases = []testParseMountSpec{
			{`c:\:d:`, "local", `d:`, `c:\`, ``, "", true, false},
			{`c:\:d:\`, "local", `d:\`, `c:\`, ``, "", true, false},
			// TODO Windows post TP4 - Add readonly support {`c:\:d:\:ro`, "local", `d:\`, `c:\`, ``, "", false, false},
			{`c:\:d:\:rw`, "local", `d:\`, `c:\`, ``, "", true, false},
			{`c:\:d:\:foo`, "local", `d:\`, `c:\`, ``, "", false, true},
			{`name:d::rw`, "local", `d:`, ``, `name`, "local", true, false},
			{`name:d:`, "local", `d:`, ``, `name`, "local", true, false},
			// TODO Windows post TP4 - Add readonly support {`name:d::ro`, "local", `d:`, ``, `name`, "local", false, false},
			{`name:c:`, "", ``, ``, ``, "", true, true},
			{`driver/name:c:`, "", ``, ``, ``, "", true, true},
		}
	} else {
		cases = []testParseMountSpec{
			{"/tmp:/tmp1", "", "/tmp1", "/tmp", "", "", true, false},
			{"/tmp:/tmp2:ro", "", "/tmp2", "/tmp", "", "", false, false},
			{"/tmp:/tmp3:rw", "", "/tmp3", "/tmp", "", "", true, false},
			{"/tmp:/tmp4:foo", "", "", "", "", "", false, true},
			{"name:/named1", "", "/named1", "", "name", "local", true, false},
			{"name:/named2", "external", "/named2", "", "name", "external", true, false},
			{"name:/named3:ro", "local", "/named3", "", "name", "local", false, false},
			{"local/name:/tmp:rw", "", "/tmp", "", "local/name", "local", true, false},
			{"/tmp:tmp", "", "", "", "", "", true, true},
		}
	}

	for _, c := range cases {
		m, err := volume.ParseMountSpec(c.bind, c.driver)
		if c.fail {
			if err == nil {
				t.Fatalf("Expected error, was nil, for spec %s\n", c.bind)
			}
			continue
		}

		if m == nil || err != nil {
			t.Fatalf("ParseMountSpec failed for spec %s driver %s error %v\n", c.bind, c.driver, err.Error())
			continue
		}

		if m.Destination != c.expDest {
			t.Fatalf("Expected destination %s, was %s, for spec %s\n", c.expDest, m.Destination, c.bind)
		}

		if m.Source != c.expSource {
			t.Fatalf("Expected source %s, was %s, for spec %s\n", c.expSource, m.Source, c.bind)
		}

		if m.Name != c.expName {
			t.Fatalf("Expected name %s, was %s for spec %s\n", c.expName, m.Name, c.bind)
		}

		if m.Driver != c.expDriver {
			t.Fatalf("Expected driver %s, was %s, for spec %s\n", c.expDriver, m.Driver, c.bind)
		}

		if m.RW != c.expRW {
			t.Fatalf("Expected RW %v, was %v for spec %s\n", c.expRW, m.RW, c.bind)
		}
	}
}
