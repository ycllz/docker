// +build windows

package system

// DefaultPathEnv is a hack to be able to ENV Path=c:\someapp;$Path on Windows.
// But it's really not any worse than the hack done on Linux :)
const DefaultPathEnv = `C:\Windows\system32;C:\Windows;C:\Windows\System32\Wbem;C:\Windows\System32\WindowsPowerShell\v1.0\`
