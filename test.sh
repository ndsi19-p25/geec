#!/bin/bash
echo "shutting down geth processes"
pkill geth
sleep 1

./clear.sh
echo "clearing workspace"
sleep 1
./bootnode.sh
echo "starting bootnode"
for i in `seq 1 3`; do
    sleep 1
    echo -n "."
done 
echo " "

echo "creating accounts"
python ./create_account.py

echo "starting geth node"
./start.sh 00
