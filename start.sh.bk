if [ ! -f bootnode.log ];then
    echo "please run bootnode.sh first"
    exit
fi

ip=147.8.88.32

bootnode_addr=enode://"$(grep enode bootnode.log|tail -n 1|awk -F '://' '{print $2}'|awk -F '@' '{print $1}')""@$ip:30301"
if [ "$1" == "" ];then
    echo "node id is empty, please use: start.sh <node_id>";
    exit
fi
no=$1
datadir=data
DIRECTORY=$datadir/$no
mkdir -p $datadir
if [ ! -d "$DIRECTORY" ]; then
    echo "initiating node...."
    ./build/bin/geth --datadir $DIRECTORY --debug --verbosity 4 init ./genesis.json
fi
./build/bin/geth --datadir $DIRECTORY --debug --verbosity 4 --networkid 930412 --ipcdisable --port 619$no --rpc --rpccorsdomain "*" --rpcport 81$no --bootnodes $bootnode_addr console 
