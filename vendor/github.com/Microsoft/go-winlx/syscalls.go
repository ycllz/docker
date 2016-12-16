package winlx

//go:generate go run $GOROOT/src/syscall/mksyscall_windows.go -output zsyscalls.go syscalls.go

//sys LxCreat(name *uint16, mode uint32, info *CreateInfo, handle *syscall.Handle) (status NtStatus) = ntposixapi.LxCreat
//sys LxOpen(name *uint16, flags uint32, mode uint32, info *CreateInfo, handle *syscall.Handle) (status NtStatus) = ntposixapi.LxOpen
//sys LxLink(oldName *uint16, newName *uint16) (status NtStatus) = ntposixapi.LxLink
//sys LxClose(handle syscall.Handle) (status NtStatus) = ntposixapi.LxClose
//sys LxRead(handle syscall.Handle, buf []byte, bytesRead *uint32) (status NtStatus) = ntposixapi.LxRead
//sys LxWrite(handle syscall.Handle, buf []byte, bytesWritten *uint32) (status NtStatus) = ntposixapi.LxWrite
