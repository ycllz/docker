// +build windows

package lcow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
)

type fileinfo struct {
	NameVar    string
	SizeVar    int64
	ModeVar    os.FileMode
	ModTimeVar int64
	IsDirVar   bool
}

func (f *fileinfo) Name() string       { return f.NameVar }
func (f *fileinfo) Size() int64        { return f.SizeVar }
func (f *fileinfo) Mode() os.FileMode  { return f.ModeVar }
func (f *fileinfo) ModTime() time.Time { return time.Unix(0, f.ModTimeVar) }
func (f *fileinfo) IsDir() bool        { return f.IsDirVar }
func (f *fileinfo) Sys() interface{}   { return nil }

func (l *lcowfs) stat(path string, cmd string) (os.FileInfo, error) {
	logrus.Debugf("REMOTEFS: %s %s", cmd, path)

	if err := l.startVM(); err != nil {
		return nil, err
	}

	output := &bytes.Buffer{}
	process, err := l.currentSVM.config.RunProcess(fmt.Sprintf("remotefs %s %s", cmd, path), nil, output, nil)
	if err != nil {
		return nil, err
	}
	process.Close()

	var fi fileinfo
	if err := json.Unmarshal(output.Bytes(), &fi); err != nil {
		return nil, err
	}

	logrus.Debugf("XXX: GOT STRUCT: %v\n", fi)
	return &fi, nil
}

func (l *lcowfs) Stat(p string) (os.FileInfo, error) {
	return l.stat(p, "stat")
}

func (l *lcowfs) Lstat(p string) (os.FileInfo, error) {
	return l.stat(p, "lstat")
}
