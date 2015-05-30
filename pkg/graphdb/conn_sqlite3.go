package graphdb

import "database/sql"

func NewSqliteConn(root string) (*Database, error) {
	checkDevBuild()
	conn, err := sql.Open("sqlite3", root)
	if err != nil {
		return nil, err
	}
	return NewDatabase(conn)
}
