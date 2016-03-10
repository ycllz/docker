package libcontainerd

import (
	"sync"

	"github.com/Sirupsen/logrus"
)

func (c *client) lock(id string) {
	c.Lock()
	if _, ok := c.containerMutexes[id]; !ok {
		c.containerMutexes[id] = &sync.Mutex{}
	}
	c.Unlock()
	c.containerMutexes[id].Lock()
}

func (c *client) unlock(id string) {
	c.Lock()
	if l, ok := c.containerMutexes[id]; ok {
		l.Unlock()
	} else {
		logrus.Warnf("unlock of non-existing mutex: %s", id)
	}
	c.Unlock()
}

// must hold a lock for c.ID
func (c *client) appendContainer(cont *container) {
	c.mapMutex.Lock()
	c.containers[cont.id] = cont
	c.mapMutex.Unlock()
}
func (c *client) deleteContainer(friendlyName string) {
	c.mapMutex.Lock()
	delete(c.containers, friendlyName)
	c.mapMutex.Unlock()
}
