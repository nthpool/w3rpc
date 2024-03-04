package w3backend

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
	"github.com/lmittmann/w3/w3vm"
)

type W3Engine struct {
}

// APIs implements consensus.Engine.
func (*W3Engine) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	panic("unimplemented")
}

// Author implements consensus.Engine.
func (*W3Engine) Author(header *types.Header) (common.Address, error) {
	return common.MaxAddress, nil
}

// CalcDifficulty implements consensus.Engine.
func (*W3Engine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	panic("unimplemented")
}

// Close implements consensus.Engine.
func (*W3Engine) Close() error {
	panic("unimplemented")
}

// Finalize implements consensus.Engine.
func (*W3Engine) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, withdrawals []*types.Withdrawal) {
	panic("unimplemented")
}

// FinalizeAndAssemble implements consensus.Engine.
func (*W3Engine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt, withdrawals []*types.Withdrawal) (*types.Block, error) {
	panic("unimplemented")
}

// Prepare implements consensus.Engine.
func (*W3Engine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	panic("unimplemented")
}

// Seal implements consensus.Engine.
func (*W3Engine) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	panic("unimplemented")
}

// SealHash implements consensus.Engine.
func (*W3Engine) SealHash(header *types.Header) common.Hash {
	panic("unimplemented")
}

// VerifyHeader implements consensus.Engine.
func (*W3Engine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	panic("unimplemented")
}

// VerifyHeaders implements consensus.Engine.
func (*W3Engine) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header) (chan<- struct{}, <-chan error) {
	panic("unimplemented")
}

// VerifyUncles implements consensus.Engine.
func (*W3Engine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	panic("unimplemented")
}

type W3Backend struct {
	client *w3.Client
	db     *db
	head   *types.Block
}

func NewW3Backend(client *w3.Client, db *db) *W3Backend {
	return &W3Backend{client: client, db: db}
}

func (b *W3Backend) SetHead(block *types.Block) {
	b.head = block
}

// BlockByHash implements tracers.Backend.
func (b *W3Backend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	var block types.Block
	err := b.client.Call(eth.BlockByHash(hash).Returns(&block))
	return &block, err
}

// BlockByNumber implements tracers.Backend.
func (b *W3Backend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	var block types.Block
	var bn *big.Int
	if number.Int64() != rpc.LatestBlockNumber.Int64() {
		bn = big.NewInt(number.Int64())
	}
	if b.head != nil && (number.Int64() == rpc.LatestBlockNumber.Int64() || b.head.NumberU64() == uint64(number.Int64())) {
		return b.head, nil
	}
	err := b.client.Call(eth.BlockByNumber(bn).Returns(&block))
	return &block, err
}

// ChainConfig implements tracers.Backend.
func (*W3Backend) ChainConfig() *params.ChainConfig {
	return params.MainnetChainConfig
}

// ChainDb implements tracers.Backend.
func (*W3Backend) ChainDb() ethdb.Database {
	panic("unimplemented")
}

// Engine implements tracers.Backend.
func (*W3Backend) Engine() consensus.Engine {
	return &W3Engine{}
}

// GetTransaction implements tracers.Backend.
func (*W3Backend) GetTransaction(context.Context, common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	panic("unimplemented")
}

// HeaderByNumber implements tracers.Backend.
func (b *W3Backend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	var header types.Header
	var bn *big.Int
	if number.Int64() != rpc.LatestBlockNumber.Int64() {
		bn = big.NewInt(number.Int64())
	}
	if b.head != nil && b.head.NumberU64() == uint64(number.Int64()) {
		return b.head.Header(), nil
	}
	err := b.client.Call(eth.HeaderByNumber(bn).Returns(&header))
	return &header, err
}

// RPCGasCap implements tracers.Backend.
func (*W3Backend) RPCGasCap() uint64 {
	return 0
}

// StateAtBlock implements tracers.Backend.
func (b *W3Backend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	var db state.Database
	if b.head == nil || block.NumberU64() < b.head.NumberU64() {
		fetcher := w3vm.NewRPCFetcher(b.client, block.Number())
		db = NewDB(fetcher, NewBadgerKV())
	} else {
		db = b.db
	}
	sdb, err := state.New(common.Hash{}, db, nil)
	if err != nil {
		return nil, nil, err
	}
	return sdb, func() {}, nil
}

// StateAtTransaction implements tracers.Backend.
func (*W3Backend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	panic("unimplemented")
}

func (b *W3Backend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	var header types.Header
	err := b.client.Call(eth.HeaderByHash(hash).Returns(&header))
	return &header, err
}

func (b *W3Backend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	var block *types.Block
	var err error
	hash, ok := blockNrOrHash.Hash()
	if ok {
		block, err = b.BlockByHash(ctx, hash)
		if err != nil {
			return nil, nil, err
		}
	} else {
		number, _ := blockNrOrHash.Number()
		block, err = b.BlockByNumber(ctx, number)
		if err != nil {
			return nil, nil, err
		}
	}
	sdb, _, err := b.StateAtBlock(ctx, block, 0, nil, false, false)
	if err != nil {
		return nil, nil, err
	}
	return sdb, block.Header(), nil
}

func (b *W3Backend) GetHeader(hash common.Hash, _ uint64) *types.Header {
	h, err := b.HeaderByHash(context.Background(), hash)
	if err != nil {
		panic(err)
	}
	return h
}
