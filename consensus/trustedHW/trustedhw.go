package trustedHW


import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"math/big"
	"github.com/ethereum/go-ethereum/rpc"
	//"github.com/naoina/toml/ast"
	//"time"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/core/thwCore"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/node"
	"time"
	"fmt"
)

var (
	// errUnknownBlock is returned when the list of signers is requested for a block
	// that is not part of the local blockchain.
	errUnknownBlock = errors.New("unknown block")
	errNoCommittee = errors.New("not a committee member")
	errNoLeader = errors.New("not leader, cannot generate a block")
)



type TrustedHW struct{
	config *params.THWConfig
	InitialAccounts []common.Address

	Mux *event.TypeMux
	Coinbase common.Address

	FakeFirstTermSet int



	InitChan chan interface{}
	initDone bool
	Registered chan interface{}

	ConsensusIP string
	ConsensusPort string

	thwState *core.THWState

	validateTimeout int
	backoffTime int

	eth thwCore.ThwMiner

	txnPerBlock int
	txnDataLen int

	breakdown bool

	last_seal_finsih_time time.Time
	last_leader bool
}

func New (coinbase common.Address, nodecfg *node.Config, config *params.THWConfig, mux *event.TypeMux, eth thwCore.ThwMiner) *TrustedHW{
	//set missing configs


	thw := new(TrustedHW)
	thw.config = config
	//
	//
	//go validator_thread_func(thw.validate_blocks, thw.validate_abort, thw.validate_errors)
	thw.Mux = mux


	thw.FakeFirstTermSet = 0


	thw.ConsensusIP = nodecfg.ConsensusIP
	thw.ConsensusPort = nodecfg.ConsensusPort
	thw.Coinbase = coinbase

	thw.eth = eth


	thw.InitChan = make(chan interface{}, 2)
	thw.initDone = false
	thw.Registered = make(chan interface{}, 2)

	thw.validateTimeout = config.ValidateTimeout

	thw.backoffTime = config.BackoffTime

	thw.txnPerBlock = config.TxnPerBlk
	thw.txnDataLen = config.TxnSize //byte

	thw.breakdown = config.BreakDown

	log.THW("Created THW consensus Engine",
		"Coin Base", coinbase,
		"IP", thw.ConsensusIP,
		"port", thw.ConsensusPort,
		"validate Timeout", thw.validateTimeout,
		"backofftime", thw.backoffTime)


	return thw
}


/*
Bootstarp the initial state.
Cannot be called in the New method because the state is not created yet.
 */
func (thw *TrustedHW) Bootstrap (chain consensus.ChainReader){
	state := chain.GetThwState()
	thw.thwState = state.(*core.THWState)

	thw.thwState.NewTerm(1, 0, 0)

	// a new goroutine to handle the registeration.
	go thw.thwState.Register(thw.Mux, thw.ConsensusIP, thw.ConsensusPort)
}


//put the author (the leader of the committee)
func (thw *TrustedHW) Author(header *types.Header) (common.Address, error){
	return header.Coinbase, nil
}


//used to verify header downloaded from other peers.
func (thw *TrustedHW) VerifyHeader (chain consensus.ChainReader, header *types.Header, seal bool) error {

	err := thw.verifyHeader(chain, header, nil);

	log.THW("Verfied Header", "Blk", header.Number.Uint64(), "ERR", err)
	return err
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications.
//
// XS: its an async function.
func (thw *TrustedHW) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	go func() {
		for i, header := range headers {
			err := thw.verifyHeader(chain, header, headers[:i])

			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}


// verifyHeader checks whether a header conforms to the consensus rules.The
// caller may optionally pass in a batch of parents (ascending order) to avoid
// looking those up from the database. This is useful for concurrently verifying
// a batch of new headers.
func (thw *TrustedHW) verifyHeader(chain consensus.ChainReader, header *types.Header, parents []*types.Header) error {
	//step 1: Sanity check.

	if header.Number == nil {
		return errUnknownBlock
	}
	number := header.Number.Uint64()
	//already in the local chain
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	//same as ethash, check ancestor first
	var parent *types.Header

	if parents == nil || len(parents) == 0 {
		parent = chain.GetHeader(header.ParentHash, number-1)
		if parent == nil {
			return consensus.ErrUnknownAncestor
		}
	}else{
		parent = parents[0]
	}
	//What to do about the parent.


	//Step 2: check author is in the committee list

	//Step 3: check the verifier's signature

	//Step 4: check the author's signature.



	return nil
}



func (thw *TrustedHW) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	//does not support uncles
	if len(block.Uncles()) > 0 {
		return errors.New("uncles not allowed")
	}
	return nil
}

//double check the seal of an outgoing message.
func (thw *TrustedHW) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	//It is currently a double check.
	return nil
}

func (thw *TrustedHW) Prepare(chain consensus.ChainReader, header *types.Header) error {

	if thw.breakdown && thw.last_leader {
		elapsed := time.Since(thw.last_seal_finsih_time)
		fmt.Println("[Breakdown 4] local processing time", elapsed)
	}


	if ! thw.initDone {
		thw.Bootstrap(chain)
		thw.initDone = true
	}

	t:= thw.thwState.GetCurrentTerm()
	for header.Number.Uint64() > t.Start + t.Len -1 {
		log.THW("New term not started, waiting for 100ms")
		time.Sleep(10*time.Millisecond)
		t = thw.thwState.GetCurrentTerm()
	}

	header.Regs = thw.thwState.GetPendingReqs()
	//A header is prepared only when a consensus has been made.
	number := header.Number.Uint64()
	log.THW("Preparing block", "number", number )

	isCommittee := core.CheckCommittee(t, header.Coinbase)

	if !isCommittee{
		return errNoCommittee
	}
	header.Nonce = types.BlockNonce{} //empty

	header.Difficulty = big.NewInt(1)
	header.MixDigest = common.Hash{} //empty

	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}

	//Testing




	return nil
}


//ensuring no uncles are set. No
func (thw *TrustedHW) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error){


	header.Root = state.IntermediateRoot(true)
	header.UncleHash = types.CalcUncleHash(nil)
	//TODO: Whether the rewards should come from here.


	return types.NewBlock(header, txs, nil, receipts), nil

}

func (thw *TrustedHW) Seal (chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error){
	//attempt to achieve consensus.
	thw.last_leader = false
	blkNum := block.NumberU64()
	s := thw.thwState
	var rand uint64

	var term *thwCore.Term
	for {
		term = s.GetCurrentTerm()

		offset := blkNum - term.Start
		if (offset < 0 || offset >= term.Len){
			log.Warn("Already on a new term", "term.start", term.Start, "Blk", block.NumberU64())
			return nil, errors.New("Term Ended")
		}


		term.StateLock.Lock()
		log.THW("Trying to seal a block", "Block", block.NumberU64(), "State", term.Roles[offset])
		elected := false

		for {
			if term.Ended == true{
				term.StateLock.Unlock()
				return nil, errors.New("Term Ended")
			}
			if term.Roles[offset] == thwCore.Follower {
				term.StateLock.Unlock()
				return nil, errNoLeader
			} else if term.Roles[offset] == thwCore.Leader {
				rand = term.TrustRands[offset]
				term.StateLock.Unlock()
				elected = true
				//block.Header().TrustRand = term.TrustRands[offset]
				break
			} else {
				log.Warn("Role for the block not decided", "Current Status", term.Roles[offset])
				term.StateCond.Wait()
			}
		}
		if elected {
			break //break the loop
		}

		time.Sleep(50 * time.Millisecond)

	}
	header := block.Header()
	header.TrustRand = rand
	block = block.WithSeal(header)




	//block.Regs = s.GetPendingReqs()

	//Is leader, process the anti-partition check.
	req := new(thwCore.ValidateRequest)
	req.BlockNum = block.NumberU64()
	//req.Author = thw.Coinbase
	req.Retry = 1
	req.TermStart = term.Start

	t := s.GetCurrentTerm()

	t.ValState[req.BlockNum - t.Start].IsProposer =true
	t.ValState[req.BlockNum - t.Start].MaxReqRetry =1
	thw.PostEvent(core.ValidateBlockEvent{req})

	//
	var start time.Time
	if thw.breakdown {
		start = time.Now()
	}


	var fakeTxs []*types.Transaction
	fakeData := make([]byte, thw.txnDataLen)
	for i:= 0; i< thw.txnPerBlock; i++{
		txn := types.NewTransaction(0, thw.Coinbase, big.NewInt(0), 0, big.NewInt(0), fakeData)
		fakeTxs = append(fakeTxs,txn)
	}
	block.FakeTxns = fakeTxs
	//
	//elapsed := time.Since(start)
	//log.THW("Generate fake txns", "elapsed", elapsed)



	for{
		select {
		case blkNum := <-term.ValidateSuccess:
			if blkNum != req.BlockNum{
				log.Warn("Received Validate Success for the wrong block", "Waiting for", req.BlockNum, "Received", blkNum)
			}else{
				//The validate succeeded.
				log.THW("Verification Succeeded", "Block num", blkNum)
				if thw.breakdown {
					elapsed := time.Since(start)
					fmt.Println("[Breakdown 2] validate time", elapsed)
					thw.last_leader = true
					thw.last_seal_finsih_time = time.Now()
				}
				time.Sleep(time.Duration(thw.backoffTime) * time.Millisecond)
				return block, nil;
			}
		case <- time.After(time.Duration(thw.validateTimeout) * time.Millisecond):
			//Timeout, resend the validate.
			threshold := t.ValState[req.BlockNum - t.Start].Threshold
			if t.ValState[req.BlockNum - t.Start].Replies.Size() >= threshold && threshold != -1 {
				return block, nil
			}
			log.Warn("Waiting for Verfication timed out",
				"Block Num", req.BlockNum,
				"Threshold", threshold,
				"Reply Count", t.ValState[req.BlockNum - t.Start].Replies.Size())
			req.Retry++
			if req.Retry >= 3 {
				log.Warn("Retried 3 times")
				return block, nil
			}


			t.ValState[req.BlockNum - t.Start].MaxReqRetry = req.Retry
			thw.PostEvent(core.ValidateBlockEvent{req})
		}
	}
}
//Main function to achieve consensus.



func (thw *TrustedHW) CalcDifficulty(chain consensus.ChainReader, time uint64, parent *types.Header) *big.Int {
	//Can use this function to change the protocol parameters.

	return big.NewInt(1)
}


func (thw *TrustedHW) APIs(chain consensus.ChainReader) []rpc.API {
	return []rpc.API{{
		Namespace: "thw",
		Version:   "1.0",
		Service:   &API{chain: chain, thw:thw},
		Public:    false,
	}}
}


func (thw *TrustedHW) PostEvent(ev interface{}) error{
	return thw.Mux.Post(ev)
}

func (thw *TrustedHW)  GetEthBase() common.Address {
	return thw.Coinbase
}

func (t *TrustedHW) GetMiner() thwCore.ThwMiner{
	return t.eth
}

