// +build windows

package windows

import "fmt"

func (d *driver) GetPidsForContainer(id string) ([]int, error) {
	// TODO This is wrong, but the type of code which requires implementing
	//d.Lock()
	//active := d.activeContainers[id]
	//d.Unlock()
	//var processes []int
	//processes[0] = int(d.activeContainers[id].command.Pid)

	return nil, fmt.Errorf("GetPidsForContainer: GetPidsForContainer() not implemented")
}
