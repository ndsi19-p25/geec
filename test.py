import subprocess, json, glob
import time
import os.path
import configparser

#default values
myip = "127.0.0.1"
boostrap_node_count = 3
normal_node_count = 0
total_account  = 3 # May need some account to do manual uage


def create_accounts():
    mkdir_log_cmd = "mkdir -p data/logs"
    subprocess.run(mkdir_log_cmd, shell=True)

    nodes = list(range(total_account))
    bootstrap_accounts = []
    for i, n in enumerate(nodes):
        node = str(n).zfill(2)
        mkdircmd = "mkdir -p data/" + node
        subprocess.run(mkdircmd, shell=True)
        print("creating account " + node)
        createcmd = "./build/bin/geth account new --datadir data/" + node + " --password ./pass 1>data/logs/"+node+" 2>&1"
        print(createcmd+"\n")
        subprocess.run(createcmd, shell=True)
        if (i < boostrap_node_count):
            file = "data/" + node + "/keystore/*"
            for f in glob.glob(file):
                user = {}
                user['account'] = json.load(open(f))['address']
                user['ip'] = myip
                user['port'] = '100' + node
                bootstrap_accounts.append(user)
    print("Creating genesis.json")
    genesis = json.load(open("genesis.json.template"))

    genesis['config']['thw']['bootstrap']= bootstrap_accounts 
    
    with open('genesis.json', 'w') as fp:
        json.dump(genesis, fp, indent=4)



def start_nodes():
    

    get_bn_addr_cmd = "grep enode bootnode.log | tail -n 1 | awk -F '://' '{print $2}' | awk -F '@' '{print $1}'"
    bn_addr_str = subprocess.check_output(get_bn_addr_cmd, shell=True)
    bn_addr = "enode://" + bn_addr_str.decode('ascii').rstrip() + "@" + myip + ":30301" 

    boostrap_nodes = range(boostrap_node_count+normal_node_count)
    for n in boostrap_nodes:
        node = str(n).zfill(2)
        data_dir = "data/"+node
        print("initializing node " + node)
        init_cmd = "./build/bin/geth --datadir "+data_dir+" --debug --verbosity 4 init ./genesis.json 1>>data/logs/"+node+" 2>&1"
        print(init_cmd +"\n")
        subprocess.run(init_cmd, shell=True)

        print("starting node" + node)        
        account_file = os.listdir(data_dir+"/keystore/")[0]
        account = json.load(open(data_dir+"/keystore/"+account_file))['address']
        start_node_cmd = "./build/bin/geth --datadir "+data_dir+ " --unlock "+account+" --password ./pass --debug --verbosity 4 --networkid 930412 --ipcdisable --port 619"+node+" --rpc --rpccorsdomain \"*\" --rpcport 81"+node+" --bootnodes "+bn_addr+" --consensusPort \"100"+node+"\" --consensusIP \"127.0.0.1\"  --syncmode \"full\" --mine 1>>data/logs/"+node+" 2>&1 &"
        print(start_node_cmd+"\n")
        subprocess.run(start_node_cmd, shell=True)


def kill():
    print("[kill] Shutting down geth process!")
    pkillcmd = "pkill geth"
    subprocess.run(pkillcmd, shell=True)
    pkill9cmd = "pkill -9 geth"
    subprocess.run(pkill9cmd, shell=True)
    


    time.sleep(1)
    print("success\n")

def clear():
    print("[clear] clearing workspace")
    clearcmd = "rm -rf data/*"
    subprocess.run(clearcmd, shell=True)
    time.sleep(1)
    print("success\n")

def bootnode():
    print("starting bootnode")
    dirname = os.path.dirname(__file__)
    bootnodefile = os.path.join(dirname, '/bootnode.key')

    if os.path.isfile(bootnodefile) == False:
        genkeycmd = "./build/bin/bootnode -genkey bootnode.key"
        subprocess.run(genkeycmd, shell=True)

    killallcmd="killall bootnode"
    subprocess.run(killallcmd, shell=True)

    bncmd = "nohup ./build/bin/bootnode -nodekey=bootnode.key > bootnode.log &"
    subprocess.run(bncmd, shell=True)
    time.sleep(3)
    print("success\n")    

def parse_config():
    config = configparser.ConfigParser()
    if os.path.isfile("config.ini") == False:
        configcmd = "cp config.ini.template config.ini"
        subprocess.run(configcmd, shell=True)
        
    
    config.read("config.ini")
    
    global myip, boostrap_node_count, normal_node_count, total_account
    
    myip = config['CLUSTER']['myip']
    boostrap_node_count = int(config['CLUSTER']['BootStrapNodeCount'])
    normal_node_count = int(config['CLUSTER']['NormalNodeCount'])
    total_account =  int(config['CLUSTER']['NAccount'])
    if total_account < boostrap_node_count + normal_node_count:
        total_account = boostrap_node_count + normal_node_count
    print("Loaded config file\n")

def main():
    parse_config()

    kill()
    clear()
    bootnode()

    create_accounts()

    start_nodes()



if __name__ == "__main__":
    main()
