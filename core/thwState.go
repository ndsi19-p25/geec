package core

import (
	"github.com/ethereum/go-ethereum/common"
	"sync"
	"fmt"
	"errors"
	"encoding/binary"
	"github.com/ethereum/go-ethereum/core/thwCore"
	"github.com/ethereum/go-ethereum/log"
	//"time"
	"time"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/consensus/trustedHW/election"
	"encoding/hex"
	"net"
	"strconv"
	"github.com/emirpasic/gods/maps/treemap"
	"github.com/ethereum/go-ethereum/event"
	"github.com/emirpasic/gods/maps/hashmap"
	"github.com/ethereum/go-ethereum/p2p"
	"sync/atomic"
)




const(
	//FIXIT: THIS IS VERY VERY BAD. I COPIED THE CONST HERE TO REDUCE CIRCULAR DEPENDENCY.
	ValidateAcceptMsg = 0x12
	ValidateRejectMsg = 0x13
)


var(
	ErrNoCandidate = errors.New("not a Candidate")
	ErrInitFailed  = errors.New("THW State Init Failed")
	ErrInvalidReg  = errors.New("invalid Registration Transaction Format")
)


type THWState struct {
	hc *HeaderChain
	mu sync.Mutex
	//members
	candidateList *treemap.Map    //key: addr, value: *Candidate
	candidateCount uint64

	CurrentTerm *thwCore.Term
	CurrentTermLock sync.Mutex


	//parameters

	//event loop for handling state transition
	newBlockChannel chan *types.Block


	Coinbase common.Address

	pendingReg *treemap.Map

	Registered chan interface{}

	MaxBlock uint64


	trustRandList *treemap.Map
	trustRandLock sync.Mutex

	eth thwCore.ThwMiner

	/*
	Parameters::
	 */

	 //On average, there is 1 committee every ``x'' candidates
	nCommittee uint64
	//One committee can serve ``x'' terms
	committeeMaxTerm uint64

	//On average, the max number of validator
	nValidator uint64
	//The threshold of validating
	validatorThreshold float32
	//Committee Timeout, in s.
	committeeTimeout int
	//Reg Timeout
	regTimeout int
	//Election Timeout
	electionTimeout int
	//Max Reg to be put into each block
	MaxRegPerBlk int

	breakdown bool
}

func (s *THWState) SetCurrentTerm(term *thwCore.Term){
	s.CurrentTermLock.Lock()
	defer s.CurrentTermLock.Unlock()

	s.CurrentTerm = term

}

func (s *THWState) GetCurrentTerm()(*thwCore.Term){
	s.CurrentTermLock.Lock()
	defer s.CurrentTermLock.Unlock()

	return s.CurrentTerm
}

func (s *THWState) SetTrustRand(blknum uint64, rand uint64){
	s.trustRandLock.Lock()
	defer s.trustRandLock.Unlock()
	s.trustRandList.Put(blknum, rand)
}

func (s *THWState) GetTrustRand(blknum uint64) (rand uint64,exist bool){
	s.trustRandLock.Lock()
	defer s.trustRandLock.Unlock()

	r, exist := s.trustRandList.Get(blknum)
	if exist {
		return r.(uint64), true
	} else {
		return 0, false
	}

}


/*
The simple comparator functions used for compare
the address as key for tree map.
 */
func addressComparator(a, b interface{}) int{
	addr1 := a.(common.Address)
	addr2 := b.(common.Address)

	for i := 0; i < common.AddressLength; i++{
		if addr1[i] < addr2[i]{
			return -1
		}else if addr1[i] > addr2[i] {
			return 1
		}
	}
	return 0
}

func uint64Comparator(a, b interface{}) int{
	x1 := a.(uint64)
	x2 := b.(uint64)

	if x1 < x2{
		return  -1
	}else if x1 > x2{
		return 1
	}else{
		return 0
	}
}

func (thws *THWState) Init(headerchain interface{}, coinbase common.Address) error { //TODO: set parameters
	thws.mu.Lock()
	defer thws.mu.Unlock()

	thws.candidateList = treemap.NewWith(addressComparator)
	thws.pendingReg = treemap.NewWith(addressComparator)
	thws.trustRandList = treemap.NewWith(uint64Comparator)
	thws.SetTrustRand(uint64(0), uint64(0))


	hc, ok := headerchain.(*HeaderChain)
	if !ok {
		return ErrInitFailed
	}
	thws.hc = hc


	//Default value.
	thws.candidateCount = 0
	thws.nCommittee = 1
	thws.nValidator = 1
	thws.validatorThreshold = 0.5
	thws.MaxRegPerBlk = 100
	thws.committeeTimeout = 100
	thws.regTimeout = 100

	thws.Coinbase = coinbase


	//the eventLoop
	thws.newBlockChannel = make(chan *types.Block, 1024000)

	thwConfig := hc.Config().THW
	if (thwConfig == nil){
		log.Crit("Failed to parse the THW config in genesis file")
	}

	for _, node := range(thwConfig.BootstrapNodes) {
		candidate := new(thwCore.Candidate)
		candidate.JoinRound = 0
		decoded, _ := hex.DecodeString(node.Account)
		copy(candidate.Addr[:],decoded)
		copy(candidate.Referee[:], decoded)
		candidate.Ip = net.ParseIP(node.IpStr)
		port, err := strconv.Atoi(node.PortStr)
		if err != nil{
			log.Crit("Wrong Port format", "value", port)
		}
		candidate.Port = port
		candidate.TTL = 1000
		thws.AddCandidate(candidate)
	}

	thws.nValidator = uint64(thwConfig.ValidatorRatio)
	thws.nCommittee = uint64(thwConfig.CommitteeRatio)
	thws.validatorThreshold = thwConfig.ValidateThreshold
	thws.committeeMaxTerm = uint64(thwConfig.TermLen)
	thws.committeeTimeout = thwConfig.CommitteeTimeout
	thws.MaxRegPerBlk = thwConfig.MaxRegPerBlk
	thws.regTimeout = thwConfig.RegTimeout
	thws.electionTimeout = thwConfig.ElectionTimeout
	thws.breakdown = thwConfig.BreakDown

	log.THW("State initialized",
		"Coinbase", coinbase,
		"validator ratio", thws.nValidator,
		"committeeRatio", thws.nCommittee,
		"validatorThreshold", thws.validatorThreshold,
		"committeeMaxTerm", thws.committeeMaxTerm,
		"committeeTimeout", thws.committeeTimeout,
		"MaxRegPerBlk", thws.MaxRegPerBlk,
		"regTimeout", thws.regTimeout,
		"electionTimeout", thws.electionTimeout)

	thws.Registered = make(chan interface{}, 1024000)

	thws.eth = hc.engine.GetMiner()


	go thws.eventLoop()


	return nil
}



/* This method should be called when the lock is held */
func (thws *THWState) AddCandidate(candidate *thwCore.Candidate) error{
	ret, found := thws.candidateList.Get(candidate.Addr)
	if found == true{
		//found an candidate
		c, ok := ret.(*thwCore.Candidate)
		if !ok {
			fmt.Println("Wrong type in the hash map")
			panic("Wrong type in the hash map")
		}
		//renew the cointract
		c.TTL = c.TTL + candidate.TTL
	}else{
		//not found in the list
		thws.candidateList.Put(candidate.Addr, candidate)
		thws.candidateCount++
	}
	log.THW("Add Candidate", "addr", candidate.Addr, "candidate count", thws.candidateCount)

	return nil
}

/*
IMPORTANT: This method is called when holding the lock
 */
func (s *THWState) getAllCommittee(seed uint64) []*thwCore.Candidate{
	var list []*thwCore.Candidate
	//defer log.THW("Get all committee", "size", len(list))
	it := s.candidateList.Iterator()

	size := uint64(s.candidateList.Size())
	if size < s.nCommittee {
		//All candidates are committee.
		for it.Begin(); it.Next(); {
			cand, success := it.Value().(*thwCore.Candidate)
			if success == false {
				log.Crit("Failed to cast candidate for getAllCommittee")
			}
			list = append(list, cand)
		}
		return list

	}else {
		start := seed % size

		if start+s.nCommittee > size {
			/*
			0...ncommittee-size+start-1		total ncommittee-size+start
			start....size-1 (end)  			total (size-start)
			 */
			count := uint64(0)
			for it.Begin(); it.Next() && count < s.nCommittee-size+start; count++ {
				cand, success := it.Value().(*thwCore.Candidate)
				if success == false {
					log.Crit("Failed to cast candidate for getAllCommittee")
				}
				list = append(list, cand)
			}

			count = uint64(0)

			for it.End(); it.Prev() && count < size-start; count++{
				cand, success := it.Value().(*thwCore.Candidate)
				if success == false {
					log.Crit("Failed to cast candidate for getAllCommittee")
				}
				list = append(list, cand)
			}
			return list
		} else {
			//start ....  start+ncommittee-1
			index := uint64(0)
			for it.Begin(); it.Next() && index < start+s.nCommittee; index++ {
				if index < start {
					continue
				}
				cand, success := it.Value().(*thwCore.Candidate)
				if success == false {
					log.Crit("Failed to cast candidate for getAllCommittee")
				}
				list = append(list, cand)
			}
			return list

		}
	}
}

func (s *THWState) getValidatorCount() uint64{
	size := uint64(s.candidateList.Size())
	if size < s.nValidator{
		return size
	}else{
		return s.nValidator
	}
}


func addrToInt (address common.Address) uint64{
	return binary.BigEndian.Uint64(address[0:8]) +
		binary.BigEndian.Uint64(address[8:16]) + uint64(binary.BigEndian.Uint32(address[16:20]))
}

func (thws *THWState) FakeConsensus(addr common.Address, number uint64) (bool, error) {
	log.THW("Doing Fake Consensus", "addr", addr)

	if _, ret := thws.candidateList.Get(addr); !ret {
		return false, ErrNoCandidate
	}

	mine := addrToInt(addr)

	candidates := thws.candidateList.Keys()
	for _, c := range candidates{
		x, ok := c.(common.Address)
		if !(ok){
			log.Error("Wrong type from candidate list")
		}else{
			if his:= addrToInt(x); mine < his{ //not the biggest
				log.THW("found addr larger than me", "my addr", addr, "my int", mine, "his addr", x, "his int", his)
				return false, nil
			}
		}
	}
	time.Sleep(2*time.Second)
	return true, nil
}

func (thws *THWState) CandidateCount() uint64{
	return thws.candidateCount
}


/*
IMPORTANT: This method is called when the lock is holding.
 */
func (thws *THWState) NewTerm (startBlk uint64, termLen uint64, seed uint64) error {
	term := new(thwCore.Term)

	atomic.StoreInt32(&term.ShouldEnd, 0)

	term.Start = startBlk
	if termLen == 0 {
		termLen = thws.committeeMaxTerm
	}
	term.Len = termLen

	term.Seed = seed
	term.Ended = false

	list := thws.getAllCommittee(seed)
	term.Committees = list


	log.THW("New Term!",
		"start", startBlk,
		"len", termLen,
		"Candidate Count", thws.CandidateCount(),
		"Committee Size", len(list))


	term.Roles = make([]thwCore.Role, termLen)
	//term.ValidateCount = make([]uint64, termLen)
	term.TrustRands = make([]uint64, termLen)


	term.ValState = make([]thwCore.ValidateState, termLen)

	term.ValidateChannel = make(chan thwCore.ValidateReply, 1024000)



	for i := uint64(0); i<termLen; i++{
		term.ValState[i].Replies= hashmap.New()
		term.ValState[i].Threshold = -1
		term.ValState[i].Succeeded = false
	}

	term.StateCond = sync.NewCond(&term.StateLock)

	term.ValidateAbort = make(chan bool, 1024000)
	term.ValidateSuccess = make(chan uint64, 1024000);
	term.IsCommittee = CheckCommittee(term, thws.Coinbase)

	//Create the new paxos group
	//
	//
	// .
	if term.IsCommittee {

		param := new(election.GroupParams)

		param.Account = thws.Coinbase.Str()
		param.CommitteeCount = len(list)
		param.Term_len = termLen
		param.Start_blk = startBlk
		param.Ipstrs = make([]string, param.CommitteeCount)
		param.Ports = make([]int, param.CommitteeCount)
		param.Timeoutms = thws.electionTimeout
		for i, can := range (list) {
			param.Ipstrs[i] = can.Ip.String()
			param.Ports[i] = can.Port
			if addressComparator(thws.Coinbase, can.Addr) == 0 {
				param.Offset = i
			}
		}

		term.PhxGroup = election.NewGroup(param)
		term.Wg.Add(1)
	}



	thws.SetCurrentTerm(term)
	// Only the committee need to determine the status
	if term.IsCommittee{
		term.ProcessWg.Add(1)
		go thws.processTerm(term)
	}

	// all node validate the replies.
	go thws.processValidateReply(term)
	term.Wg.Add(1)

	return nil
}


func (s *THWState) IsValidator (blknum uint64) (result bool){
	//First check whether I am candidate
	//check against my coin addreess.
	var seed uint64
	exist := false
	seed, exist = s.GetTrustRand(blknum-1);

	count := 0

	for exist == false && count < 20 {
		log.THW("Falied to get trust rand, waiting for 10ms", "blk", blknum-1, "retry", count)
		time.Sleep(10 * time.Millisecond)
		seed, exist = s.GetTrustRand(blknum-1);
		count ++
	}
	if exist == false{
		log.THW("Cannot get seed, give up", "Blk", blknum)
		return false
	}


	//XSTODO: copied from getallcommittee.
	//Copied from get
	it := s.candidateList.Iterator()

	size := uint64(s.candidateList.Size())
	if size < s.nValidator {
		//All candidates validator
		return true

	}else {
		start := seed % size

		if start+s.nValidator > size {
			/*
			0...nVal-size+start-1		total nVal-size+start
			start....size-1 (end)  			total (size-start)
			 */
			count := uint64(0)
			for it.Begin(); it.Next() && count < s.nValidator-size+start; count++ {
				cand, success := it.Value().(*thwCore.Candidate)
				if success == false {
					log.Crit("Failed to cast candidate for getAllCommittee")
				}
				if addressComparator(cand.Addr, s.Coinbase) == 0{
					return  true
				}
			}

			count = uint64(0)

			for it.End(); it.Prev() && count < size-start; count++{
				cand, success := it.Value().(*thwCore.Candidate)
				if success == false {
					log.Crit("Failed to cast candidate for getAllCommittee")
				}
				if addressComparator(cand.Addr, s.Coinbase) == 0{
					return  true
				}
			}
			return false
		} else {
			//start ....  start+ncommittee-1
			index := uint64(0)
			for it.Begin(); it.Next() && index < start+s.nValidator; index++ {
				if index < start {
					continue
				}
				cand, success := it.Value().(*thwCore.Candidate)
				if success == false {
					log.Crit("Failed to cast candidate for getAllCommittee")
				}
				if addressComparator(cand.Addr, s.Coinbase) == 0{
					return  true
				}
			}
			return false
		}
	}



}




func (s *THWState) Validate (rw p2p.MsgReadWriter, req *thwCore.ValidateRequest){
	isValidator := s.IsValidator(req.BlockNum)
	if isValidator{
		log.THW("I am validator, sending reply", "Blk", req.BlockNum)
	}else{
		log.THW("I am not validator", "Blk", req.BlockNum)
		return
	}


	var reply thwCore.ValidateReply
	reply.BlockNum = req.BlockNum
	reply.Author = s.Coinbase
	reply.Retry = req.Retry
	reply.TermStart = req.TermStart

	valResult := true

	if (isValidator) {
		if valResult {
			p2p.Send(rw, ValidateAcceptMsg, reply)
		} else {
			p2p.Send(rw, ValidateRejectMsg, reply)
		}

	}
}



//determine the role for each block in the term of service.
func (thws *THWState) processTerm (term *thwCore.Term){
	defer term.ProcessWg.Done()
	defer log.THW("Processing Term finished", "start", term.Start)


	log.THW("start processing Term")
	if term.Start == 1 {
		time.Sleep(10 * time.Second)
	}

	var i uint64
	for i = 0; i<term.Len; i++{
		//log.THW("Processing Term", "i", i)



		if atomic.LoadInt32(&term.ShouldEnd) == 1{
			return
		}

		max := atomic.LoadUint64(&thws.MaxBlock)
		if max > i + term.Start {
			log.THW("No need to elect for that")
			term.StateLock.Lock()
			term.Roles[i] = thwCore.Follower
			term.StateCond.Broadcast()
			term.StateLock.Unlock()
			term.TrustRands[i] = 0
			continue
		}
		var start time.Time
		if thws.breakdown{
			start = time.Now()
		}


		success, rand := term.PhxGroup.Elect(i + term.Start)
		if success == -1{
			//try again
			log.THW("Electing for leader timed out")
			i--
			continue
		}
		term.StateLock.Lock()
		if success == 1 {
			term.Roles[i] = thwCore.Leader
			term.TrustRands[i] = rand
			term.StateCond.Broadcast()
			term.StateLock.Unlock()

			if thws.breakdown{
				elapsed := time.Since(start)
				fmt.Println("[Breakdown 1] election time", elapsed)
			}


 		}else{
 			term.Roles[i] = thwCore.Follower
			term.StateCond.Broadcast()
			term.StateLock.Unlock()
		}
	}
}


func (thws *THWState) processValidateReply(term *thwCore.Term){
	defer term.Wg.Done()

	for{
		select{
		case reply := <-term.ValidateChannel:
			//log.THW("Received Validate Reply")
			if atomic.LoadInt32(&term.ShouldEnd) == 1{
				log.THW("Ending Process Validate Reply")
				return
			}
			//log.THW("Received Validate Reply", "Blknum", reply.BlockNum)
			if reply.BlockNum < term.Start || reply.BlockNum > term.Start + term.Len {
				//log.THW("Processing Validating Reply", "counter", c)
				log.Warn("[Validate] Received Reply not in current term", "blk number", reply.BlockNum)
				continue
			}else{
				//count the reply
				offset := reply.BlockNum - term.Start

				term.ValState[offset].Mu.Lock()

				_, exist := term.ValState[offset].Replies.Get(reply.Author)
				if exist {
					term.ValState[offset].Mu.Unlock()
					continue
				}

				term.ValState[offset].Replies.Put(reply.Author, reply.Retry)
				term.ValState[offset].Mu.Unlock()



				var threshold int
				if term.ValState[offset].Threshold == -1{
					threshold = int(float32(thws.getValidatorCount()) * thws.validatorThreshold)
					term.ValState[offset].Threshold = threshold
				}else{
					threshold = term.ValState[offset].Threshold
				}

				if term.ValState[offset].Replies.Size() >= threshold && term.ValState[offset].Succeeded== false{
					term.ValState[offset].Succeeded = true
					term.ValidateSuccess <- reply.BlockNum
				}
			}
		case <- term.ValidateAbort:
			log.THW("Received Validate Abort")
			return
		}
		//log.THW("Processing Validating Reply", "counter", c)

	}
}

//This method must be single-threaded (sync) to prevent problems.
func (thws *THWState) NotifyNewBlock (blk *types.Block){
	thws.newBlockChannel <- blk
	//Think about it: whether it need to wait the handle finish.
}

func (thws *THWState) eventLoop(){
	for{
		select{
		case blk := <- thws.newBlockChannel:
			thws.handleNewBlock(blk)
		case <- time.After(time.Duration(thws.committeeTimeout) * time.Second):
			thws.handleTimeout()
		}
	}
}
/*
IMPORTANT: This function is called when the state lock is helded.
 */

func endTerm (term *thwCore.Term){

	atomic.StoreInt32(&term.ShouldEnd, 1)
	//log.THW("Term Should end", "term address", term)


	term.ValidateAbort <- true

	if term.IsCommittee{
		term.ProcessWg.Wait()
		term.PhxGroup.DestroyGroup()
		term.Wg.Done()
	}

	term.Wg.Wait()

	log.THW("Validate thread exited")

	term.StateLock.Lock()
	term.Ended = true
	term.StateCond.Broadcast()
	term.StateLock.Unlock()


	log.THW("Successfully Ended the term", "start", term.Start, "len", term.Len)

}



//The blocks are supposed to come ``in order''.
func (thws *THWState) handleNewBlock (blk *types.Block){
	thws.mu.Lock()
	defer thws.mu.Unlock()

	log.THW("handleNewBlock", "Blocknum", blk.NumberU64(), "N Txns", blk.FakeTxns.Len())

	thws.SetTrustRand(blk.NumberU64(), blk.Header().TrustRand)
	log.THW("Set trust rand succeeded", "Blocknum", blk.NumberU64())


	if blk.NumberU64() > atomic.LoadUint64(&thws.MaxBlock) {
		atomic.StoreUint64(&thws.MaxBlock, blk.NumberU64())
	}
	/*
	Handle the registration first
	Because those candidate registered can be in the list.
	 */
	for _, reg := range blk.Header().Regs{
		log.THW("Received Registration", "blk", blk.NumberU64(), "ip", reg.IpStr, "port", reg.PortStr)
		//** remove the pending registration request
		thws.pendingReg.Remove(reg.Account)
		//Add the candidate to list
		cand := new(thwCore.Candidate)
		cand.Referee = reg.Referee
		cand.Addr = reg.Account
		port, err := strconv.Atoi(reg.PortStr)
		if err != nil{
			log.Warn("Failed to parse Port string, ignore", "Port", reg.PortStr)
			continue
		}
		cand.Port = port
		cand.Ip = net.ParseIP(reg.IpStr)
		cand.TTL = 1000
		thws.AddCandidate(cand)
		//If my account is registered, stop registering
		if addressComparator(thws.Coinbase, reg.Account) == 0{
			thws.Registered <- true
		}

	}

	term := thws.GetCurrentTerm()

	lastTermEnd := term.Start + term.Len - 1
	if blk.NumberU64() ==  lastTermEnd && blk.NumberU64() != 0 {
		log.Warn("Going to do committee change")
		if term != nil {
			endTerm(term)
		}
		thws.NewTerm(lastTermEnd + 1, term.Len, blk.Header().TrustRand)
	}





}

func (s *THWState) handleTimeout(){
	log.Warn("Timeout!!!! Going to do committee change")
	s.mu.Lock()
	defer s.mu.Unlock()



	term := s.GetCurrentTerm()

	if term != nil {
		endTerm(term)
	}
	s.eth.StopMining()
	max := atomic.LoadUint64(&s.MaxBlock)
	s.NewTerm(max+1, term.Len, s.hc.CurrentHeader().TrustRand)
	s.eth.StartMining(true)
}
/*
Append the newly received reg request in the pending requests tree map
This function is not written considering efficiency.
 */
func (thws *THWState) AppendRegReq(registratoin *types.Registratoin){
	thws.mu.Lock()
	defer thws.mu.Unlock()

	r, exist := thws.pendingReg.Get(registratoin.Account)
	if exist {
		record := r.(*types.Registratoin)
		if record.IpStr == registratoin.IpStr && record.PortStr == registratoin.PortStr {
			log.THW("Received an Registration request already known")
			return
		}
	}
	thws.pendingReg.Put(registratoin.Account, registratoin)
	log.THW("Added an Registration request", "Account", registratoin.Account, "IP", registratoin.IpStr, "Port", registratoin.PortStr)
}

func (s* THWState) GetPendingReqs() (regs []*types.Registratoin){
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0

	it := s.pendingReg.Iterator()
	for it.Begin(); it.Next(); {
		reg, success := it.Value().(*types.Registratoin)
		if success == false {
			log.Crit("Failed to cast from pendingRequest")
		}
		regs = append(regs, reg)
		count++
		if count >= s.MaxRegPerBlk{
			break
		}
	}
	return regs
}

func (thws *THWState) Register (mux *event.TypeMux,	ipstr string, portStr string){

	//if it is the bootstrap node, stop the registration.

	_, found := thws.candidateList.Get(thws.Coinbase)
	if found{
		log.THW("Already a candidate, no need to register")
		return
	} else{
		log.THW("I am not a candidate, trying to register", "list", thws.candidateList.Keys(), "Coinbase", thws.Coinbase)
	}


	reg := new(types.Registratoin)
	reg.PortStr = portStr
	reg.IpStr = ipstr
	reg.Referee = thws.Coinbase
	reg.Account = thws.Coinbase
	reg.Signature = types.FakeSignature[:]

	i := 0
	for{
		select{
		case <-time.After( time.Duration(thws.regTimeout) * time.Second):
			i++ //wait for more time.
			mux.Post(RegisterReqEvent{reg})
			log.THW("Registering thw", "times", i+1)
		case <-thws.Registered:
			log.THW("Registered as Candidate")
			return
		}
	}

}

func CheckCommittee(t *thwCore.Term, addr common.Address) bool{
	for _, c := range t.Committees{
		if addressComparator(c.Addr, addr) == 0 {
			return true
		}
	}
	return false
}

