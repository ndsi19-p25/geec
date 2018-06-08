import subprocess, json, glob

myip = "127.0.0.1"

nodes = ["00", "01", "02"]
bootstrap_accounts = []
for node in nodes:
    mkdircmd = "mkdir -p data/" + node
    subprocess.run(mkdircmd, shell=True)
    print("creating account " + node)
    createcmd = "./build/bin/geth account new --datadir data/" + node + " --password ./pass"
    subprocess.run(createcmd, shell=True)
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
