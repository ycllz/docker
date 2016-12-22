#!/bin/sh
set -e

# Get the device name
id="$1":0:0:"$2"
devname=$(ls -l /sys/block/sd* | grep -o "$id/block/sd.*" | sed "s-$id/block-/dev-g")

# Make a ext4 file system
yes | mkfs.ext4 "$devname"

# mount the device to a new folder
foldername="$3"
mkdir "$foldername"
mount "$devname" "$foldername"
