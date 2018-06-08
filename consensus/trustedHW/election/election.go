package election

/*
#include "lib/node.h"
#include <stdlib.h>
#cgo LDFLAGS: -lpthread -L${SRCDIR}/lib -lnode
*/
import "C"
import (
	"github.com/ethereum/go-ethereum/log"
	"unsafe"
)


type PaxosGroup struct{
	Group  *C.Term_t
	offset int
	Timeoutms int
foo string 
}




type GroupParams struct {
	Ipstrs         []string
	Ports          []int
	Offset         int
	Account        string
	CommitteeCount int
	Start_blk      uint64
	Term_len       uint64
	Timeoutms 	   int
}


func NewGroup(p *GroupParams) *PaxosGroup {

	g := new(PaxosGroup)


	g.Timeoutms = p.Timeoutms

	if g.Timeoutms == 0{
		g.Timeoutms = 500 //default
	}


	c_ipstrs := C.makeCharArray(C.int(p.CommitteeCount))
	for i, s := range p.Ipstrs {
		C.setArrayString(c_ipstrs, C.CString(s), C.int(i))
	}
	c_ports := make([]C.int, p.CommitteeCount)
	for i, port := range p.Ports{
		c_ports[i] = C.int(port)
	}
	c_account :=C.CString(p.Account)
	g.Group = C.New_Node(c_ipstrs, &c_ports[0], C.int(p.Offset), c_account , C.int(p.CommitteeCount), C.ulong(p.Start_blk), C.ulong(p.Term_len))
	C.free(unsafe.Pointer(c_account))
	C.freeCharArray(c_ipstrs, C.int(p.CommitteeCount))

	g.offset = p.Offset
	return g
}




/*
Return Value:
0: Election Failed
1: Elected
-1: Election Timed out.
 */

func (g *PaxosGroup) Elect (blk uint64) (int, uint64) {
	log.THW("Electing", "blk", blk)
	var r C.ulong
	ret := C.elect(g.Group, C.ulong(blk), &r, C.int(g.Timeoutms))
	rand := uint64(r)

	if ret == 1 {
		log.THW("Elected as leader","block", blk, "rand",  rand);
		return 1, rand
	} else if ret == -1 {
		log.THW("Election Timed out", "block", blk);
		return -1, rand
	} else {
		log.THW("Election failed", "block", blk);
		return 0, rand
	}
}

func (g *PaxosGroup) DestroyGroup(){
	C.killGroup(g.Group)
	log.THW("Successfully Killed Paxos group.")
}
