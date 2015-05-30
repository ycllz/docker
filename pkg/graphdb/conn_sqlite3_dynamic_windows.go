// +build cgo,windows,dynamicsqlite

package graphdb

import (
	_ "code.google.com/p/gosqlite/sqlite3" // registers sqlite
	"github.com/Sirupsen/logrus"
)

func checkDevBuild() {

	//	dynamicsqlite should not be used for release builds. It is purely for
	//	development purposes as it takes some 20 seconds less to build docker
	//	on Windows with dynamic linking.

	logrus.Warnf("Windows is using SQLite3 dynamic linking. THIS IS FOR DEVELOPMENT ONLY")
}
