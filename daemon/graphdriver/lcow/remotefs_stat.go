package lcow

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"encoding/gob"

	"github.com/Sirupsen/logrus"
)

type fileinfo struct {
	NameVar    string
	SizeVar    int64
	ModeVar    os.FileMode
	ModTimeVar time.Time
	IsDirVar   bool
}

func (f *fileinfo) Name() string       { return f.NameVar }
func (f *fileinfo) Size() int64        { return f.SizeVar }
func (f *fileinfo) Mode() os.FileMode  { return f.ModeVar }
func (f *fileinfo) ModTime() time.Time { return f.ModTimeVar }
func (f *fileinfo) IsDir() bool        { return f.IsDirVar }
func (f *fileinfo) Sys() interface{}   { return nil }

func (d *lcowfs) stat(path string, cmd string) (os.FileInfo, error) {
	logrus.Debugf("REMOTEFS: %s %s", cmd, path)

	output := &bytes.Buffer{}
	if err := d.config.RunProcess(fmt.Sprintf("remotefs %s %s", cmd, path), nil, output); err != nil {
		return nil, err
	}

	var fi fileinfo
	dec := gob.NewDecoder(output)
	if err := dec.Decode(&fi); err != nil {
		return nil, err
	}

	logrus.Debugf("XXX: GOT STRUCT: %v\n", fi)
	return &fi, nil
}

func (d *lcowfs) Stat(p string) (os.FileInfo, error) {
	return d.stat(p, "stat")
}

func (d *lcowfs) Lstat(p string) (os.FileInfo, error) {
	return d.stat(p, "lstat")
}
