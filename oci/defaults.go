package oci

import (
	"os"
	"runtime"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func sPtr(s string) *string      { return &s }
func iPtr(i int64) *int64        { return &i }
func u32Ptr(i int64) *uint32     { u := uint32(i); return &u }
func fmPtr(i int64) *os.FileMode { fm := os.FileMode(i); return &fm }

// DefaultSpec returns the default spec used by docker for the current Platform
func DefaultSpec() specs.Spec {
	return DefaultOSSpec(runtime.GOOS)
}

// DefaultOSSpec returns the spec for a given OS
func DefaultOSSpec(osName string) specs.Spec {
	if osName == "windows" {
		return DefaultWindowsSpec()
	} else if osName == "solaris" {
		return DefaultSolarisSpec()
	} else {
		return DefaultLinuxSpec()
	}
}

// DefaultWindowsSpec returns default spec used by docker for Windows
func DefaultWindowsSpec() specs.Spec {
	return specs.Spec{
		Version: specs.Version,
		Platform: specs.Platform{
			OS:   "windows",
			Arch: runtime.GOARCH,
		},
		Windows: &specs.Windows{},
	}
}

// DefaultSolarisSpec returns default oci spec used by docker for Solaris
func DefaultSolarisSpec() specs.Spec {
	s := specs.Spec{
		Version: "0.6.0",
		Platform: specs.Platform{
			OS:   "SunOS",
			Arch: runtime.GOARCH,
		},
	}
	s.Solaris = &specs.Solaris{}
	return s
}

// DefaultLinuxSpec returns default oci spec used by docker for Linux
func DefaultLinuxSpec() specs.Spec {
	s := specs.Spec{
		Version: specs.Version,
		Platform: specs.Platform{
			OS:   "linux",
			Arch: runtime.GOARCH,
		},
	}
	s.Mounts = []specs.Mount{
		{
			Destination: "/proc",
			Type:        "proc",
			Source:      "proc",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/dev",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755"},
		},
		{
			Destination: "/dev/pts",
			Type:        "devpts",
			Source:      "devpts",
			Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"},
		},
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "ro"},
		},
		{
			Destination: "/sys/fs/cgroup",
			Type:        "cgroup",
			Source:      "cgroup",
			Options:     []string{"ro", "nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/dev/mqueue",
			Type:        "mqueue",
			Source:      "mqueue",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
	}
	s.Process.Capabilities = []string{
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FSETID",
		"CAP_FOWNER",
		"CAP_MKNOD",
		"CAP_NET_RAW",
		"CAP_SETGID",
		"CAP_SETUID",
		"CAP_SETFCAP",
		"CAP_SETPCAP",
		"CAP_NET_BIND_SERVICE",
		"CAP_SYS_CHROOT",
		"CAP_KILL",
		"CAP_AUDIT_WRITE",
	}

	s.Linux = &specs.Linux{
		MaskedPaths: []string{
			"/proc/kcore",
			"/proc/latency_stats",
			"/proc/timer_list",
			"/proc/timer_stats",
			"/proc/sched_debug",
			"/sys/firmware",
		},
		ReadonlyPaths: []string{
			"/proc/asound",
			"/proc/bus",
			"/proc/fs",
			"/proc/irq",
			"/proc/sys",
			"/proc/sysrq-trigger",
		},
		Namespaces: []specs.Namespace{
			{Type: "mount"},
			{Type: "network"},
			{Type: "uts"},
			{Type: "pid"},
			{Type: "ipc"},
		},
		// Devices implicitly contains the following devices:
		// null, zero, full, random, urandom, tty, console, and ptmx.
		// ptmx is a bind-mount or symlink of the container's ptmx.
		// See also: https://github.com/opencontainers/runtime-spec/blob/master/config-linux.md#default-devices
		Devices: []specs.Device{},
		Resources: &specs.Resources{
			Devices: []specs.DeviceCgroup{
				{
					Allow:  false,
					Access: sPtr("rwm"),
				},
				{
					Allow:  true,
					Type:   sPtr("c"),
					Major:  iPtr(1),
					Minor:  iPtr(5),
					Access: sPtr("rwm"),
				},
				{
					Allow:  true,
					Type:   sPtr("c"),
					Major:  iPtr(1),
					Minor:  iPtr(3),
					Access: sPtr("rwm"),
				},
				{
					Allow:  true,
					Type:   sPtr("c"),
					Major:  iPtr(1),
					Minor:  iPtr(9),
					Access: sPtr("rwm"),
				},
				{
					Allow:  true,
					Type:   sPtr("c"),
					Major:  iPtr(1),
					Minor:  iPtr(8),
					Access: sPtr("rwm"),
				},
				{
					Allow:  true,
					Type:   sPtr("c"),
					Major:  iPtr(5),
					Minor:  iPtr(0),
					Access: sPtr("rwm"),
				},
				{
					Allow:  true,
					Type:   sPtr("c"),
					Major:  iPtr(5),
					Minor:  iPtr(1),
					Access: sPtr("rwm"),
				},
				{
					Allow:  false,
					Type:   sPtr("c"),
					Major:  iPtr(10),
					Minor:  iPtr(229),
					Access: sPtr("rwm"),
				},
			},
		},
	}

	return s
}
