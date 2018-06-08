import os, subprocess
import sys

list = os.listdir("data/logs")
list.sort()
for _, file in enumerate(list):
    print("log: " + file)
    filepath = os.path.join("data/logs", file)
    grepcmd = "cat " + filepath + "| grep -i -n -a -E \"" + sys.argv[1] +"\""
    print(grepcmd)
    subprocess.run(grepcmd, shell=True)
    print("")
