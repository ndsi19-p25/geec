import subprocess, json, glob
import time
import os.path
import configparser


config_file_name = 'config-test.json'
#!! No trailing /
code_path = None
accounts = []
bootstrap_accounts = []

#default values
config = None
machines = []


def create_accounts():
    print("\nCreating accounts")

    boostrap_node_count = config['bootstrap_nodes']
    normal_node_count = config['normal_nodes'] 



    total_node = boostrap_node_count+ normal_node_count
    global bootstrap_accounts
    global accounts


    for i in range(total_node):
        node = str(i).zfill(2)
        machine = machines[i % len(machines)]
        print("creating account " + node + " on machine: " + machine)
        createcmd = ["ssh", "%s" % machine, "cd %s && ./build/bin/geth account new --datadir data/%s --password ./pass 1>data/logs/%s 2>&1" % (code_path, node, node)]
        print(createcmd)
        subprocess.run(createcmd)

        cat_cmd = ["ssh", "%s" % machine, "cat %s/data/%s/keystore/*" % (code_path, node)]
        result = subprocess.run(cat_cmd, stdout = subprocess.PIPE)
        account = json.loads(result.stdout.decode('ascii'))['address']
        accounts.append(account)
        if (i < boostrap_node_count):
            user = {}
            user['account'] = account
            user['ip'] = machine
            user['port'] = '100' + node
            bootstrap_accounts.append(user)
 
def start_nodes():
    print("\nStarting nodes")

    boostrap_node_count = config['bootstrap_nodes']
    normal_node_count = config['normal_nodes'] 
    my_ip = config['my_ip']

    get_bn_addr_cmd = "grep enode bootnode.log | tail -n 1 | awk -F '://' '{print $2}' | awk -F '@' '{print $1}'"
    bn_addr_str = subprocess.check_output(get_bn_addr_cmd, shell=True)
    bn_addr = "enode://%s@%s:30301" % (bn_addr_str.decode('ascii').rstrip(), my_ip)

    max_peer = config['max_peer']
    verbos = config['verbosity']
    if config['print_debug'] == True:
        debug = "--debug"
    else:
        debug = ""

    for i in range(boostrap_node_count+normal_node_count):
        node = str(i).zfill(2)
        machine = machines[i % len(machines)]
        print("initializing node " + node)
        init_cmd = ["ssh", "%s" % machine, "cd %s && ./build/bin/geth --datadir data/%s --debug --verbosity 4 init ./genesis.json 1>>data/logs/%s 2>&1" % (code_path, node, node)]
        print(init_cmd)
        subprocess.run(init_cmd)

        print("starting node" + node)        
        start_node_cmd = ["ssh", "%s" % machine, "sh -c \'cd %s && nohup ./build/bin/geth --datadir data/%s --unlock %s --password ./pass %s --verbosity %d --networkid 930412 --ipcdisable --port 619%s --rpc --rpccorsdomain \"*\" --rpcport 81%s --bootnodes %s --consensusPort \"100%s\" --consensusIP %s  --syncmode \"full\" --maxpeers %d --mine 1>>data/logs/%s 2>&1 &\'" % (code_path, node, accounts[i], debug, verbos, node, node, bn_addr, node, machine,  max_peer, node)]
        print(start_node_cmd)
        subprocess.run(start_node_cmd)

def bootnode():
    print("\nstarting bootnode")
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

def prepare():
    for machine in machines:
        print("Preparing machine: %s" % machine)
        cmd = ["ssh", "%s" % machine, "cd %s && rm -rf data/*; pkill geth; pkill -9 geth; mkdir -p data/logs " % code_path]
        print(cmd)
        subprocess.run(cmd)

def parse_config():
    global config
    config = json.load(open(config_file_name))

    global machines 
    for i in range(len(config['cluster'])):
        machines.append(config['cluster'][i]['ip'])
    print(machines)

    global code_path
    code_path = config['code_path']

def generate_json():
    print("\nCreating genesis.json")
    print(bootstrap_accounts)
 
    genesis = json.load(open("genesis.json.template"))

    genesis['config']['thw']['bootstrap']= bootstrap_accounts 
    for key, value in config['parameters'].items():
        genesis['config']['thw'][key] = value


    with open('genesis.json', 'w') as fp:
        json.dump(genesis, fp, indent=4)

    for machine in machines:
        print("Copying genesis.json: %s" % machine)
        scp_cmd = ["scp", "genesis.json", "%s:%s/genesis.json" % (machine, code_path)]
        print(scp_cmd)
        subprocess.run(scp_cmd)

    


def main():
    parse_config()


    prepare()

    bootnode()

    create_accounts()

    generate_json()

    start_nodes()



if __name__ == "__main__":
    main()
