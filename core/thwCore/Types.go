package thwCore

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/trustedHW/election"
	"net"
	"sync"
	"github.com/emirpasic/gods/maps/hashmap"
)

type Candidate struct {
	Referee   common.Address
	Addr      common.Address
	JoinRound uint64 //at which round the candidate joined as candidate
	TTL       uint64 //the ``total'' term for the candidate
	Ip        net.IP
	Port      int
}

type Role int

const (
	Empty Role = iota
	Follower
	Leader
)

/*
The first term should start from 1.
The term is responsible for block (start) (start + 1) .... (start + len - 1)
The term need to be changed when the block (start + len - 1) is received
Based on the Rand on the block (start + len - 1)
*/

type Term struct {
	Start uint64
	Len   uint64
	Seed  uint64
	Roles []Role
	//ValidateCount []uint64  replaced with the hashset to dedup

	TrustRands  []uint64
	PhxGroup    *election.PaxosGroup
	StateLock   sync.Mutex
	StateCond   *sync.Cond
	IsCommittee bool

	Wg              sync.WaitGroup
	ValidateAbort   chan bool
	ValidateSuccess chan uint64

	Ended 	bool
	ShouldEnd int32
	ProcessWg sync.WaitGroup

	ValState []ValidateState

	ValidateChannel chan ValidateReply


	Committees []*Candidate
}




type ValidateState struct{
	//Requestor   *p2p.MsgReadWriter
	Replies     *hashmap.Map
	IsProposer  bool
	ProposerRetry uint64 //Very ugly, see the code. Ensure the proposer only answer once
	MaxReqRetry uint64
	Threshold   int
	Mu          sync.Mutex
	Succeeded 	bool
	//MaxReplyRetry int
}

type ValidateRequest struct {
	BlockNum uint64
	Author   common.Address
	Retry uint64
	TermStart uint64
}

type ValidateReply struct {
	BlockNum uint64
	Author   common.Address
	Accepted bool
	Retry uint64
	TermStart uint64
}

type ThwMiner interface{
	StopMining()
	StartMining(local bool) error
	IsMining() bool
}