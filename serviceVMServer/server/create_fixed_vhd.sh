#!/bin/bash

roundpow2()
{
    n="$1"
    
    if [[ "$n" == 0 ]]; then
        printf "1"
    fi

    if (( ( n & (n - 1) ) == 0 )); then 
        printf "$n"
    fi

    m=1
    while (( m < n )); do
        (( m <<= 1))
    done
    printf "$m"
}

fix_inode()
{
    inode="$1"
    
    # Assume that we need 20% more inodes than we actually need and round to
    # the nearest power of two.
    inode=$(( inode * 6 / 5 ))
    roundpow2 $inode
}

fix_size()
{
    size="$1"
    inode="$2"
    
    # Add 256 bytes per inode
    size=$(( size + inode * 256 ))
    
    # Assume we need 20% more size
    size=$(( size * 6 / 5 ))
    
    # Do some checks between inode + size
    if [[ $size -lt  $(( inode * 1024 )) ]]; then
        size=$(( inode * 1024 ))
    fi

    if [[ $size -lt "65536" ]]; then
        size=$(( 64 * 1024 ))
    fi

    # Align to 8k 
    if [[ $(( size % 8192 )) != 0 ]]; then
        size=$(( size + 8192 - size % 8192 ))
    fi

    printf "$size"
}


srcpath="$1"
mntpath="$2"
devpath="$3"

if [[ "$#" != 3 ]]; then
    echo "Usage: $0 <tarpath> <fldrpath> <devpath>"
fi

awkscript='
BEGIN { n=0; s=0 }
{
    if (substr($1, 0, 1) != "h")
    {
        n += 1
        s += $3
    }
}
END { print n; print s }' 

# First, get the size and the inodes required
data=($(tar tvf "$srcpath" | awk "$awkscript"))
oldsize=${data[1]}
inode=$(fix_inode ${data[0]})
size=$(fix_size $oldsize $inode)

# Second, set up the loopback device
count=$(( size /  8192 ))
dd if=/dev/zero of="$devpath" bs=8K count=$count > /dev/null 2>&1
if [[ $? != 0 ]]; then
    exit 1
fi
 
loop=$(losetup -f --show "$devpath")
if [[ $? != 0 ]]; then
    exit 1
fi

# Second, create file system and mount
mkfs.ext4 "$loop" -N "$num" > /dev/null 2>&1
if [[ $? != 0 ]]; then
    losetup -d "$loop"
    exit 1
fi

mount "$loop" "$mntpath"
if [[ $? != 0 ]]; then
    losetup -d "$loop"
    exit 1
fi

# Untar the files
tar xf "$srcpath" -C "$mntpath"
retval="$?"

# Last, close loopback device
umount "$loop"
losetup -d "$loop"

# Output the fixed size
if [[ "$retval" == 0 ]]; then
    echo "$oldsize"
    printf "$size"
fi
exit "$retval"
