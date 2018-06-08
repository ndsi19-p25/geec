package types

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

/*
The special address used for registration.
Similar logic for the zero address used for cointract creation
 */

var	(
	RegAddr = [20]byte{0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff,0xff}
	FakeSignature = [5]byte{0x00, 0x01, 0x02, 0x03, 0x04}
	)

type Registratoin struct{
	Account common.Address
	Referee common.Address

	IpStr string
	PortStr string

	Signature []byte //The signature of the Referee
}
//
//func (r *Registratoin) Encode() (bytes []byte){
//	b, err := rlp.EncodeToBytes(r)
//	if err != nil{
//		log.Crit("Failed to serialize the Registration to Bytes")
//	}
//	return b
//}
//
//func DecodeReg(bytes []byte) (r *Registratoin){
//	err := rlp.DecodeBytes(bytes, r)
//	if (err != nil){
//		log.Crit("Failed to Decode Registration")
//	}
//	return r
//}

type Registrations []*Registratoin

func (r Registrations) Len() int {return len(r)}

func (r Registrations) GetRlp (i int) []byte {
	enc, _ := rlp.EncodeToBytes(r[i])
	return enc
}

