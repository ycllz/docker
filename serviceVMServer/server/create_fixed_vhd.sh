#!/bin/bash

size="$1"
num="$2"
devpath="$3"
mntpath="$4"
srcpath="$5"

# First, set up the loopback device
count=$(( size /  8192 ))
dd if=/dev/zero of="$devpath" bs=8K count=$count
if [[ $? != 0 ]]; then
    exit 1
fi
 
loop=$(losetup -f --show "$devpath")
if [[ $? != 0 ]]; then
    exit 1
fi

# Second, create file system and copy files
mkfs.ext4 "$loop" -N "$num"
if [[ $? != 0 ]]; then
    losetup -d "$loop"
    exit 1
fi

mount "$loop" "$mntpath"
if [[ $? != 0 ]]; then
    losetup -d "$loop"
    exit 1
fi

# Move the files over.
mv "$srcpath"/* "$mntpath"
retval="$?"

# Last, close loopback device
umount "$loop"
losetup -d "$loop"

exit "$retval"
