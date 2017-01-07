#!/bin/bash
rm -rf log.txt

mntpath="$1"
devpath="$2"
size="$3"
numInodes="$4"

if [[ "$#" != 4 ]]; then
    echo "Usage: $0 <mntpath> <devpath> <size> <numInodes>"
    exit 1
fi

# First, set up the loopback device
count=$(( size /  65536 ))
dd if=/dev/zero of="$devpath" bs=64K count=$count >> log.txt 2>&1
if [[ $? != 0 ]]; then
    exit 1
fi
 
loop=$(losetup -f --show "$devpath")
if [[ $? != 0 ]]; then
    exit 1
fi

# Second, create file system and mount
mkfs.ext4 -O ^has_journal,^resize_inode "$loop" -N "$numInodes" >> log.txt 2>&1
if [[ $? != 0 ]]; then
    losetup -d "$loop"
    exit 1
fi

mount "$loop" "$mntpath"
if [[ $? != 0 ]]; then
    losetup -d "$loop"
    exit 1
fi

# Set up to unmount + destroy loopback device when server calls umount
losetup -d "$loop"
exit 0
