package daemon

import "github.com/docker/docker/container"

// sqliteMigration performs the link graph DB migration. No-op on Windows
func (daemon *Daemon) sqliteMigration(_ map[string]*container.Container) error {
	return nil
}
