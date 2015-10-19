package volume

import (
	"runtime"
	"strings"
	"testing"
)

func TestParseMountSpec(t *testing.T) {
	var (
		valid   []string
		invalid map[string]string
	)

	if runtime.GOOS == "windows" {
		valid = []string{
			`d:\`,
			`d:`,
			`d:\path`,
			`d:\path with space`,
			// TODO Windows post TP4 - readonly support `d:\pathandmode:ro`,
			`c:\:d:\`,
			`c:\windows\:d:`,
			`c:\windows:d:\s p a c e`,
			`c:\windows:d:\s p a c e:RW`,
			`c:\program files:d:\s p a c e i n h o s t d i r`,
			`0123456789name:d:`,
			`MiXeDcAsEnAmE:d:`,
			`name:D:`,
			`name:D::rW`,
			`name:D::RW`,
			// TODO Windows post TP4 - readonly support `name:D::RO`,
			`c:/:d:/forward/slashes/are/good/too`,
			// TODO Windows post TP4 - readonly support `c:/:d:/including with/spaces:ro`,
			`c:\Windows`,             // With capital
			`c:\Program Files (x86)`, // With capitals and brackets
		}
		invalid = map[string]string{
			``:                                 "Invalid volume specification: ",
			`.`:                                "Invalid volume specification: ",
			`..\`:                              "Invalid volume specification: ",
			`c:\:..\`:                          "Invalid volume specification: ",
			`c:\:d:\:xyzzy`:                    "Invalid volume specification: ",
			`c:`:                               "cannot be c:",
			`c:\`:                              `cannot be c:\`,
			`c:\notexist:d:`:                   `The system cannot find the file specified`,
			`c:\windows\system32\ntdll.dll:d:`: `Source 'c:\windows\system32\ntdll.dll' is not a directory`,
			`name<:d:`:                         `Invalid volume specification`,
			`name>:d:`:                         `Invalid volume specification`,
			`name::d:`:                         `Invalid volume specification`,
			`name":d:`:                         `Invalid volume specification`,
			`name\:d:`:                         `Invalid volume specification`,
			`name*:d:`:                         `Invalid volume specification`,
			`name|:d:`:                         `Invalid volume specification`,
			`name?:d:`:                         `Invalid volume specification`,
			`name/:d:`:                         `Invalid volume specification`,
			`d:\pathandmode:rw`:                `Invalid volume specification`,
			`con:d:`:                           `cannot be a reserved word for Windows filenames`,
			`PRN:d:`:                           `cannot be a reserved word for Windows filenames`,
			`aUx:d:`:                           `cannot be a reserved word for Windows filenames`,
			`nul:d:`:                           `cannot be a reserved word for Windows filenames`,
			`com1:d:`:                          `cannot be a reserved word for Windows filenames`,
			`com2:d:`:                          `cannot be a reserved word for Windows filenames`,
			`com3:d:`:                          `cannot be a reserved word for Windows filenames`,
			`com4:d:`:                          `cannot be a reserved word for Windows filenames`,
			`com5:d:`:                          `cannot be a reserved word for Windows filenames`,
			`com6:d:`:                          `cannot be a reserved word for Windows filenames`,
			`com7:d:`:                          `cannot be a reserved word for Windows filenames`,
			`com8:d:`:                          `cannot be a reserved word for Windows filenames`,
			`com9:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt1:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt2:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt3:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt4:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt5:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt6:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt7:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt8:d:`:                          `cannot be a reserved word for Windows filenames`,
			`lpt9:d:`:                          `cannot be a reserved word for Windows filenames`,
		}

	} else {
		valid = []string{
			"/home",
			"/home:/home",
			"/home:/something/else",
			"/with space",
			"/home:/with space",
			"relative:/absolute-path",
			"hostPath:/containerPath:ro",
			"/hostPath:/containerPath:rw",
			"/rw:/ro",
		}
		invalid = map[string]string{
			"":                "Invalid volume specification",
			"./":              "Invalid volume destination",
			"../":             "Invalid volume destination",
			"/:../":           "Invalid volume destination",
			"/:path":          "Invalid volume destination",
			":":               "Invalid volume specification",
			"/tmp:":           "Invalid volume destination",
			":test":           "Invalid volume specification",
			":/test":          "Invalid volume specification",
			"tmp:":            "Invalid volume destination",
			":test:":          "Invalid volume specification",
			"::":              "Invalid volume specification",
			":::":             "Invalid volume specification",
			"/tmp:::":         "Invalid volume specification",
			":/tmp::":         "Invalid volume specification",
			"/path:rw":        "Invalid volume specification",
			"/path:ro":        "Invalid volume specification",
			"/rw:rw":          "Invalid volume specification",
			"path:ro":         "Invalid volume specification",
			"/path:/path:sw":  "invalid mode for",
			"/path:/path:rwz": "invalid mode for",
		}
	}

	for _, path := range valid {
		if _, err := ParseMountSpec(path, "local"); err != nil {
			t.Fatalf("ParseMountSpec(`%q`) should succeed: error %q", path, err)
		}
	}

	for path, expectedError := range invalid {
		if _, err := ParseMountSpec(path, "local"); err == nil {
			t.Fatalf("ParseMountSpec(`%q`) should have failed validation. Err %v", path, err)
		} else {
			if !strings.Contains(err.Error(), expectedError) {
				t.Fatalf("ParseMountSpec(`%q`) error should contain %q, got %v", path, expectedError, err.Error())
			}
		}
	}
}

func TestSplitN(t *testing.T) {
	for _, x := range []struct {
		input    string
		n        int
		expected []string
	}{
		{`C:\foo:d:`, -1, []string{`C:\foo`, `d:`}},
		{`:C:\foo:d:`, -1, nil},
		{`/foo:/bar:ro`, 3, []string{`/foo`, `/bar`, `ro`}},
		{`/foo:/bar:ro`, 2, []string{`/foo`, `/bar:ro`}},
		{`C:\foo\:/foo`, -1, []string{`C:\foo\`, `/foo`}},

		{`d:\`, -1, []string{`d:\`}},
		{`d:`, -1, []string{`d:`}},
		{`d:\path`, -1, []string{`d:\path`}},
		{`d:\path with space`, -1, []string{`d:\path with space`}},
		{`d:\pathandmode:rw`, -1, []string{`d:\pathandmode`, `rw`}},
		{`c:\:d:\`, -1, []string{`c:\`, `d:\`}},
		{`c:\windows\:d:`, -1, []string{`c:\windows\`, `d:`}},
		{`c:\windows:d:\s p a c e`, -1, []string{`c:\windows`, `d:\s p a c e`}},
		{`c:\windows:d:\s p a c e:RW`, -1, []string{`c:\windows`, `d:\s p a c e`, `RW`}},
		{`c:\program files:d:\s p a c e i n h o s t d i r`, -1, []string{`c:\program files`, `d:\s p a c e i n h o s t d i r`}},
		{`0123456789name:d:`, -1, []string{`0123456789name`, `d:`}},
		{`MiXeDcAsEnAmE:d:`, -1, []string{`MiXeDcAsEnAmE`, `d:`}},
		{`name:D:`, -1, []string{`name`, `D:`}},
		{`name:D::rW`, -1, []string{`name`, `D:`, `rW`}},
		{`name:D::RW`, -1, []string{`name`, `D:`, `RW`}},
		{`c:/:d:/forward/slashes/are/good/too`, -1, []string{`c:/`, `d:/forward/slashes/are/good/too`}},
		{`c:\Windows`, -1, []string{`c:\Windows`}},
		{`c:\Program Files (x86)`, -1, []string{`c:\Program Files (x86)`}},

		{``, -1, nil},
		{`.`, -1, []string{`.`}},
		{`..\`, -1, []string{`..\`}},
		{`c:\:..\`, -1, []string{`c:\`, `..\`}},
		{`c:\:d:\:xyzzy`, -1, []string{`c:\`, `d:\`, `xyzzy`}},
	} {
		res := SplitN(x.input, x.n)
		if len(res) < len(x.expected) {
			t.Fatalf("input: %v, expected: %v, got: %v", x.input, x.expected, res)
		}
		for i, e := range res {
			if e != x.expected[i] {
				t.Fatalf("input: %v, expected: %v, got: %v", x.input, x.expected, res)
			}
		}
	}
}
