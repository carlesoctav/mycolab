mycolab (more utility around colab cliu)


multiple workspace/account
mycolab add work
mycolab add personal
(then i can use mycolab list to check list and mycolab use)

mycolab list [list all the user give *for the current sessions]
mycolab use [open a multiple choice where i can select]
mycolab use xx [switch to xx workspace]
mycolab ssh -> [and write the .ssh/config colab host with the selected sessions, please make it interactive where i can choose with ky jk(down up, vim keybind), and enter]

please use seperate ssh config file for this colab
https://stackoverflow.com/questions/19966721/multiple-ssh-config-files

please see the ref folder, i want u to use go and cobra, ask somehthing if it's not clear to you thanks.

for the config file let's make a symlink

put all profile on .config/mycolab/xx.json, and on "use" command the symlink
im not really sure what happens we make new profile probably make an empty file/json.
