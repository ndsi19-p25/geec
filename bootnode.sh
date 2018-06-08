#!/bin/bash
if [ ! -f bootnode.key ];then
    ./build/bin/bootnode -genkey bootnode.key
fi
killall bootnode
nohup ./build/bin/bootnode -nodekey=bootnode.key > bootnode.log&
