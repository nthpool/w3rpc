package fetcher

import (
	"encoding/json"
	"math/big"
	"sync"
	"time"

	"github.com/consensys-vertical-apps/va-mmcx-tx-sentinel-api/service/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
	"github.com/lmittmann/w3/w3types"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

// Fetcher is the interface to access account state of a blockchain.
type Fetcher interface {

	// Nonce fetches the nonce of the given address.
	Nonce(common.Address) (uint64, error)

	// Balance fetches the balance of the given address.
	Balance(common.Address) (*big.Int, error)

	// Code fetches the code of the given address.
	Code(common.Address) ([]byte, error)

	// StorageAt fetches the state of the given address and storage slot.
	StorageAt(common.Address, common.Hash) (common.Hash, error)

	// HeaderHash fetches the hash of the header with the given number.
	HeaderHash(*big.Int) (common.Hash, error)
}

type StateUpdate struct {
	TraceBlock TraceBlock[PrestateTrace]
	Block      *ethTypes.Block
}

type rpcFetcher struct {
	client      *w3.Client
	blockNumber *big.Int

	g            *singleflight.Group
	mux2         sync.RWMutex
	headerHashes map[uint64]common.Hash
	stateChan    chan *StateUpdate
	o            sync.Once
	ticker       *time.Ticker
	blockHead    uint64
}

// NewRPCFetcher returns a new [Fetcher] that fetches account state from the given
// RPC client for the given block number.
//
// Note, that the returned state for a given block number is the state after the
// execution of that block.
func NewRPCFetcher(client *w3.Client, blockNumber *big.Int) *rpcFetcher {
	return newRPCFetcher(client, blockNumber)
}

func newRPCFetcher(client *w3.Client, blockNumber *big.Int) *rpcFetcher {
	return &rpcFetcher{
		client:       client,
		blockNumber:  blockNumber,
		g:            new(singleflight.Group),
		headerHashes: make(map[uint64]common.Hash),
		stateChan:    make(chan *StateUpdate),
		ticker:       time.NewTicker(time.Second * 6),
	}
}

func (f *rpcFetcher) getLatestState() (*StateUpdate, error) {
	var block ethTypes.Block
	err := f.client.Call(eth.BlockByNumber(nil).Returns(&block))
	if err != nil {
		return nil, err
	}
	if block.NumberU64() <= f.blockHead {
		return nil, nil
	}
	res := TraceBlock[PrestateTrace]{}
	err = f.client.Call(
		GetPrestateTraceBlockCaller(types.NewBlockNumber(block.NumberU64()), &PrestateTracerConfig{DiffMode: true}, nil).Returns(&res),
	)
	if err != nil {
		return nil, err
	}
	f.blockHead = block.NumberU64()
	return &StateUpdate{res, &block}, nil
}

func (f *rpcFetcher) StateStream() <-chan *StateUpdate {
	f.o.Do(func() {
		// start polling for block updates
		go func() {
		LOOP:
			for {
				select {
				case <-f.ticker.C:
					trace, err := f.getLatestState()
					if err != nil {
						log.Err(err).Send()
						continue LOOP
					}
					if trace != nil {
						f.stateChan <- trace
					}
				}
			}
		}()
	})
	return f.stateChan
}

func (f *rpcFetcher) BlockNumber(bn *big.Int) {
	f.blockNumber.Set(bn)
}

func (f *rpcFetcher) Nonce(addr common.Address) (uint64, error) {
	acc, err := f.fetchAccount(addr)
	if err != nil {
		return 0, err
	}
	return acc.Nonce, nil
}

func (f *rpcFetcher) Balance(addr common.Address) (*big.Int, error) {
	acc, err := f.fetchAccount(addr)
	if err != nil {
		return nil, err
	}
	return acc.Balance.ToBig(), nil
}

func (f *rpcFetcher) Code(addr common.Address) ([]byte, error) {
	acc, err := f.fetchAccount(addr)
	if err != nil {
		return nil, err
	}
	return acc.Code, nil
}

func (f *rpcFetcher) StorageAt(addr common.Address, slot common.Hash) (common.Hash, error) {
	storageVal, err := f.fetchStorageAt(addr, *new(uint256.Int).SetBytes32(slot[:]))
	if err != nil {
		return common.Hash{}, err
	}
	return storageVal, nil
}

func (f *rpcFetcher) HeaderHash(blockNumber *big.Int) (common.Hash, error) {
	return f.fetchHeaderHash(blockNumber)
}

func (f *rpcFetcher) fetchAccount(addr common.Address) (*account, error) {
	accAny, err, _ := f.g.Do(string(addr[:]), func() (interface{}, error) {
		// fetch account from RPC
		var (
			nonce   uint64
			balance big.Int
			code    []byte
		)
		if err := f.call(
			eth.Nonce(addr, f.blockNumber).Returns(&nonce),
			eth.Balance(addr, f.blockNumber).Returns(&balance),
			eth.Code(addr, f.blockNumber).Returns(&code),
		); err != nil {
			return nil, err
		}
		acc := &account{
			Nonce:   nonce,
			Balance: *uint256.MustFromBig(&balance),
			Code:    code,
			Storage: make(map[uint256.Int]uint256.Int),
		}

		return acc, nil
	})
	if err != nil {
		return nil, err
	}
	return accAny.(*account), nil
}

func (f *rpcFetcher) fetchStorageAt(addr common.Address, slot uint256.Int) (common.Hash, error) {
	slotBytes := slot.Bytes32()
	storageValAny, err, _ := f.g.Do(string(append(addr[:], slotBytes[:]...)), func() (interface{}, error) {
		var storageVal common.Hash
		// fetch storage from RPC
		if err := f.call(
			eth.StorageAt(addr, common.BigToHash(slot.ToBig()), f.blockNumber).Returns(&storageVal),
		); err != nil {
			return common.Hash{}, err
		}
		return storageVal, nil
	})
	if err != nil {
		return common.Hash{}, err
	}
	return storageValAny.(common.Hash), nil
}

func (f *rpcFetcher) fetchHeaderHash(blockNumber *big.Int) (common.Hash, error) {
	hashAny, err, _ := f.g.Do(blockNumber.String(), func() (interface{}, error) {
		n := blockNumber.Uint64()

		// check if header hash is already cached
		f.mux2.RLock()
		hash, ok := f.headerHashes[n]
		f.mux2.RUnlock()
		if ok {
			return hash, nil
		}

		// fetch head hash from RPC
		var header ethTypes.Header
		if err := f.call(
			eth.HeaderByNumber(blockNumber).Returns(&header),
		); err != nil {
			return nil, err
		}
		hash = header.Hash()

		// cache account
		f.mux2.Lock()
		f.headerHashes[n] = hash
		f.mux2.Unlock()
		return hash, nil
	})
	if err != nil {
		return common.Hash{}, err
	}
	return hashAny.(common.Hash), nil
}

func (f *rpcFetcher) call(calls ...w3types.Caller) error {
	return f.client.Call(calls...)
}

////////////////////////////////////////////////////////////////////////////////////////////////////
// account /////////////////////////////////////////////////////////////////////////////////////////
////////////////////////////////////////////////////////////////////////////////////////////////////

type account struct {
	Nonce   uint64
	Balance uint256.Int
	Code    []byte
	Storage map[uint256.Int]uint256.Int
}

type accountMarshaling struct {
	Nonce   hexutil.Uint64                `json:"nonce"`
	Balance hexutil.U256                  `json:"balance"`
	Code    hexutil.Bytes                 `json:"code"`
	Storage map[hexutil.U256]hexutil.U256 `json:"storage,omitempty"`
}

func (acc account) MarshalJSON() ([]byte, error) {
	storage := make(map[hexutil.U256]hexutil.U256, len(acc.Storage))
	for slot, val := range acc.Storage {
		storage[hexutil.U256(slot)] = hexutil.U256(val)
	}
	return json.Marshal(accountMarshaling{
		Nonce:   hexutil.Uint64(acc.Nonce),
		Balance: hexutil.U256(acc.Balance),
		Code:    acc.Code,
		Storage: storage,
	})
}

func (acc *account) UnmarshalJSON(data []byte) error {
	var dec accountMarshaling
	if err := json.Unmarshal(data, &dec); err != nil {
		return err
	}

	acc.Nonce = uint64(dec.Nonce)
	acc.Balance = uint256.Int(dec.Balance)
	acc.Code = dec.Code
	acc.Storage = make(map[uint256.Int]uint256.Int)
	for slot, val := range dec.Storage {
		acc.Storage[uint256.Int(slot)] = uint256.Int(val)
	}
	return nil
}
