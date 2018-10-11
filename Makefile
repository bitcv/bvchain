#
#

CMDS = bv_node

all:
	go build ./...
	for x in $(CMDS); do (cd cmd/$$x; go install); done

