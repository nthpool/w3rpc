package fetcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/consensys-vertical-apps/va-mmcx-tx-sentinel-api/service/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/lmittmann/w3/w3types"
)

type TraceBlock[TC any] []*txTraceResult[TC]

func TraceBlockCaller[TC, TT any](method string, blockNumber *types.NumberOrBlockTag, tracerConfig *TraceConfig[TC]) *Factory[TT] {
	return NewFactory[TT](
		method,
		[]any{blockNumber.TagOrHex(), tracerConfig},
	)
}

func GetPrestateTraceBlockCaller(blockNumber *types.NumberOrBlockTag, config *PrestateTracerConfig, stateOverrides StateOverride) *Factory[TraceBlock[PrestateTrace]] {
	// prestate tracer setup
	preStateConfig := &TraceConfig[PrestateTracerConfig]{Tracer: "prestateTracer", TracerConfig: config, StateOverrides: stateOverrides}
	return TraceBlockCaller[PrestateTracerConfig, TraceBlock[PrestateTrace]]("debug_traceBlockByNumber", blockNumber, preStateConfig)
}

// borrowed from estimate gas poc

var (
	null        = []byte("null")
	errNotFound = errors.New("not found")
)

// the following is borrowed from w3
type Option[T any] func(*Factory[T])

type ArgsWrapperFunc func([]any) ([]any, error)

type RetWrapperFunc[T any] func(*T) any

type Factory[T any] struct {
	method string
	args   []any
	ret    *T

	argsWrapper ArgsWrapperFunc
	retWrapper  RetWrapperFunc[T]
}

func NewFactory[T any](method string, args []any, opts ...Option[T]) *Factory[T] {
	f := &Factory[T]{
		method: method,
		args:   args,

		argsWrapper: func(args []any) ([]any, error) { return args, nil },
		retWrapper:  func(ret *T) any { return ret },
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (f Factory[T]) Returns(ret *T) w3types.Caller {
	f.ret = ret
	return f
}

func (f Factory[T]) CreateRequest() (rpc.BatchElem, error) {
	args, err := f.argsWrapper(f.args)
	if err != nil {
		return rpc.BatchElem{}, err
	}

	return rpc.BatchElem{
		Method: f.method,
		Args:   args,
		Result: &json.RawMessage{},
	}, nil
}

func (f Factory[T]) HandleResponse(elem rpc.BatchElem) error {
	if err := elem.Error; err != nil {
		return err
	}

	ret := *(elem.Result.(*json.RawMessage))
	if len(ret) == 0 || bytes.Equal(ret, null) {
		return errNotFound
	}

	//fmt.Println(string(ret))

	if err := json.Unmarshal(ret, f.retWrapper(f.ret)); err != nil {
		return err
	}
	return nil
}

func msgArgsWrapper(slice []any) ([]any, error) {
	msg := slice[0].(*w3types.Message)
	if msg.Input != nil || msg.Func == nil {
		return slice, nil
	}

	input, err := msg.Func.EncodeArgs(msg.Args...)
	if err != nil {
		return nil, err
	}
	msg.Input = input
	slice[0] = msg
	return slice, nil
}

func WithArgsWrapper[T any](fn ArgsWrapperFunc) Option[T] {
	return func(f *Factory[T]) {
		f.argsWrapper = fn
	}
}

func WithRetWrapper[T any](fn RetWrapperFunc[T]) Option[T] {
	return func(f *Factory[T]) {
		f.retWrapper = fn
	}
}

func BlockNumberArg(blockNumber *big.Int) string {
	if blockNumber == nil {
		return "latest"
	}
	return hexutil.EncodeBig(blockNumber)
}

// CallTraceCall requests the call trace of the given message.
func TraceCaller[TC, TT any](method string, msg *w3types.Message, blockNumber *big.Int, tracerConfig *TraceConfig[TC]) w3types.CallerFactory[TT] {
	return NewFactory(
		method,
		[]any{msg, BlockNumberArg(blockNumber), tracerConfig},
		WithArgsWrapper[TT](msgArgsWrapper),
	)
}

func GetPrestateTraceCaller(msg *w3types.Message, blockNumber *big.Int, config *PrestateTracerConfig, stateOverrides StateOverride) w3types.CallerFactory[PrestateTrace] {
	// prestate tracer setup
	preStateConfig := &TraceConfig[PrestateTracerConfig]{Tracer: "prestateTracer", TracerConfig: config, StateOverrides: stateOverrides}
	return TraceCaller[PrestateTracerConfig, PrestateTrace]("debug_traceCall", msg, blockNumber, preStateConfig)
}

func GetCallTraceCaller(msg *w3types.Message, blockNumber *big.Int, config *CallTracerConfig, stateOverrides StateOverride) w3types.CallerFactory[CallTrace] {
	// call tracer setup
	callTraceConfig := &TraceConfig[CallTracerConfig]{Tracer: "callTracer", TracerConfig: config, StateOverrides: stateOverrides}
	return TraceCaller[CallTracerConfig, CallTrace]("debug_traceCall", msg, blockNumber, callTraceConfig)
}

//var blockRetWrapper = func(ret *types.Block) any { return (*rpcBlock)(ret) }

// BlockByNumber requests the block with the given number with full
// transactions. If number is nil, the latest block is requested.
func BlockByNumber(number *big.Int) w3types.CallerFactory[RPCBlock] {
	return NewFactory[RPCBlock](
		"eth_getBlockByNumber",
		[]any{BlockNumberArg(number), true},
	)
}

type State = map[common.Address]*Account

type Account struct {
	Balance *hexutil.Big                `json:"balance,omitempty"`
	Code    *hexutil.Bytes              `json:"code,omitempty"`
	Nonce   *uint64                     `json:"nonce,omitempty"`
	Storage map[common.Hash]common.Hash `json:"storage,omitempty"`
}

// txTraceResult is the result of a single transaction trace.
type txTraceResult[TC any] struct {
	TxHash common.Hash `json:"txHash"`           // transaction hash
	Result *TC         `json:"result,omitempty"` // Trace results produced by the tracer
	Error  string      `json:"error,omitempty"`  // Trace failure produced by the tracer
}

type PrestateTrace struct {
	Post State `json:"post,omitempty"`
	Pre  State `json:"pre"`
}

type CallTrace struct {
	From    common.Address `json:"from"`
	To      common.Address `json:"to"`
	Type    string         `json:"type"`
	Gas     hexutil.Uint64 `json:"gas"`
	GasUsed hexutil.Uint64 `json:"gasUsed"`
	Value   *hexutil.Big   `json:"value"`
	Input   hexutil.Bytes  `json:"input"`
	Output  hexutil.Bytes  `json:"output"`
	Error   string         `json:"error"`
	Calls   []*CallTrace   `json:"calls"`
}

type PrestateTracerConfig struct {
	DiffMode bool `json:"diffMode"` // If true, this tracer will return state modifications
}

type CallTracerConfig struct {
	OnlyTopCall bool `json:"onlyTopCall"`
	WithLog     bool `json:"withLog"`
}

// OverrideAccount indicates the overriding fields of account during the execution
// of a message call.
// Note, state and stateDiff can't be specified at the same time. If state is
// set, message execution will only use the data in the given state. Otherwise
// if statDiff is set, all diff will be applied first and then execute the call
// message.
type OverrideAccount struct {
	Nonce     *hexutil.Uint64             `json:"nonce,omitempty"`
	Code      *hexutil.Bytes              `json:"code,omitempty"`
	Balance   *hexutil.Big                `json:"balance,omitempty"`
	State     map[common.Hash]common.Hash `json:"state,omitempty"`
	StateDiff map[common.Hash]common.Hash `json:"stateDiff,omitempty"`
}

// StateOverride is the collection of overridden accounts.
type StateOverride map[common.Address]OverrideAccount

type TraceConfig[TC any] struct {
	Tracer         string        `json:"tracer"`
	TracerConfig   *TC           `json:"tracerConfig,omitempty"`
	StateOverrides StateOverride `json:"stateOverrides,omitempty"`
}

type RPCBlock struct {
	ParentHash      common.Hash         `json:"parentHash"`
	UncleHash       common.Hash         `json:"sha3Uncles"`
	Coinbase        common.Address      `json:"miner"`
	Root            common.Hash         `json:"stateRoot"`
	TxHash          common.Hash         `json:"transactionsRoot"`
	ReceiptHash     common.Hash         `json:"receiptsRoot"`
	Bloom           ethTypes.Bloom      `json:"logsBloom"`
	Difficulty      *hexutil.Big        `json:"difficulty"`
	Number          *hexutil.Big        `json:"number"`
	GasLimit        hexutil.Uint64      `json:"gasLimit"`
	GasUsed         hexutil.Uint64      `json:"gasUsed"`
	Time            hexutil.Uint64      `json:"timestamp"`
	Extra           hexutil.Bytes       `json:"extraData"`
	MixDigest       common.Hash         `json:"mixHash"`
	Nonce           ethTypes.BlockNonce `json:"nonce"`
	Transactions    []*RPCTransaction   `json:"transactions"`                             // added
	ChainID         *hexutil.Big        `json:"chainId,omitempty"`                        // non standard
	BaseFee         *hexutil.Big        `json:"baseFeePerGas,omitempty" rlp:"optional"`   // BaseFee was added by EIP-1559 and is ignored in legacy headers.
	WithdrawalsHash *common.Hash        `json:"withdrawalsRoot,omitempty" rlp:"optional"` // WithdrawalsHash was added by EIP-4895 and is ignored in legacy headers.
	ExcessDataGas   *hexutil.Big        `json:"excessDataGas,omitempty" rlp:"optional"`   // ExcessDataGas was added by EIP-4844 and is ignored in legacy headers.
}

type RPCTransaction struct {
	Type hexutil.Uint64 `json:"type"`

	ChainID              *hexutil.Big         `json:"chainId,omitempty"`
	Nonce                hexutil.Uint64       `json:"nonce"`
	To                   *common.Address      `json:"to"`
	Gas                  hexutil.Uint64       `json:"gas"`
	GasPrice             *hexutil.Big         `json:"gasPrice"`
	MaxPriorityFeePerGas *hexutil.Big         `json:"maxPriorityFeePerGas"`
	MaxFeePerGas         *hexutil.Big         `json:"maxFeePerGas"`
	MaxFeePerBlobGas     *hexutil.Big         `json:"maxFeePerBlobGas,omitempty"`
	Value                *hexutil.Big         `json:"value"`
	Input                *hexutil.Bytes       `json:"input"`
	AccessList           *ethTypes.AccessList `json:"accessList,omitempty"`
	BlobVersionedHashes  []common.Hash        `json:"blobVersionedHashes,omitempty"`
	V                    *hexutil.Big         `json:"v"`
	R                    *hexutil.Big         `json:"r"`
	S                    *hexutil.Big         `json:"s"`
	YParity              *hexutil.Uint64      `json:"yParity,omitempty"`

	// Arbitrum fields:
	From                *common.Address `json:"from,omitempty"`                // Contract SubmitRetryable Unsigned Retry
	RequestId           *common.Hash    `json:"requestId,omitempty"`           // Contract SubmitRetryable Deposit
	TicketId            *common.Hash    `json:"ticketId,omitempty"`            // Retry
	MaxRefund           *hexutil.Big    `json:"maxRefund,omitempty"`           // Retry
	SubmissionFeeRefund *hexutil.Big    `json:"submissionFeeRefund,omitempty"` // Retry
	RefundTo            *common.Address `json:"refundTo,omitempty"`            // SubmitRetryable Retry
	L1BaseFee           *hexutil.Big    `json:"l1BaseFee,omitempty"`           // SubmitRetryable
	DepositValue        *hexutil.Big    `json:"depositValue,omitempty"`        // SubmitRetryable
	RetryTo             *common.Address `json:"retryTo,omitempty"`             // SubmitRetryable
	RetryValue          *hexutil.Big    `json:"retryValue,omitempty"`          // SubmitRetryable
	RetryData           *hexutil.Bytes  `json:"retryData,omitempty"`           // SubmitRetryable
	Beneficiary         *common.Address `json:"beneficiary,omitempty"`         // SubmitRetryable
	MaxSubmissionFee    *hexutil.Big    `json:"maxSubmissionFee,omitempty"`    // SubmitRetryable
	EffectiveGasPrice   *hexutil.Uint64 `json:"effectiveGasPrice,omitempty"`   // ArbLegacy
	L1BlockNumber       *hexutil.Uint64 `json:"l1BlockNumber,omitempty"`       // ArbLegacy

	// Only used for encoding:
	Hash common.Hash `json:"hash"`
}

func (tx *RPCTransaction) ToW3Message() *w3types.Message {
	msg := &w3types.Message{
		From:  *tx.From,
		To:    tx.To,
		Nonce: uint64(tx.Nonce),
		Gas:   uint64(tx.Gas),
	}
	msg.GasPrice = big.NewInt(0)
	if tx.GasPrice != nil {
		msg.GasPrice.Set(tx.GasPrice.ToInt())
	}
	msg.GasFeeCap = big.NewInt(0)
	if tx.MaxFeePerGas != nil {
		msg.GasFeeCap.Set(tx.MaxFeePerGas.ToInt())
	}
	msg.GasTipCap = big.NewInt(0)
	if tx.MaxPriorityFeePerGas != nil {
		msg.GasTipCap.Set(tx.MaxPriorityFeePerGas.ToInt())
	}
	msg.Value = big.NewInt(0)
	if tx.Value != nil {
		msg.Value.Set(tx.Value.ToInt())
	}
	if tx.Input != nil {
		msg.Input = common.CopyBytes(*tx.Input)
	}
	if tx.AccessList != nil {

	}
	return msg
}

func (a *Account) CodeHash() common.Hash {
	var codeHash []byte
	if a.Code == nil || len(*a.Code) == 0 {
		codeHash = ethTypes.EmptyCodeHash[:]
	} else {
		codeHash = crypto.Keccak256(*a.Code)
	}
	return common.BytesToHash(codeHash)
}

/*
func dynamicFeeTx(dec *RPCTransaction) *types.Transaction {
	var inner types.TxData
	var itx types.DynamicFeeTx
	inner = &itx
	itx.ChainID = (*big.Int)(dec.ChainID)
	itx.Nonce = uint64(*dec.Nonce)
	if dec.To != nil {
		itx.To = dec.To
	}
	itx.Gas = uint64(*dec.Gas)
	itx.GasTipCap = (*big.Int)(dec.MaxPriorityFeePerGas)
	itx.GasFeeCap = (*big.Int)(dec.MaxFeePerGas)
	itx.Value = (*big.Int)(dec.Value)
	itx.Data = *dec.Input
	if dec.AccessList != nil {
		itx.AccessList = *dec.AccessList
	}
	itx.V = (*big.Int)(dec.V)
	itx.R = (*big.Int)(dec.R)
	itx.S = (*big.Int)(dec.S)
	return types.NewTx(inner)
}

func (b *RPCBlock) UnmarshalJSON(data []byte) error {
	type rpcBlockTxs struct {
		Transactions []*RPCTransaction `json:"transactions"`
	}

	var header types.Header
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}

	var blockTxs rpcBlockTxs
	if err := json.Unmarshal(data, &blockTxs); err != nil {
		return err
	}

	txs := []*types.Transaction{}
	for _, rtx := range blockTxs.Transactions {
		txs = append(txs, dynamicFeeTx(rtx))
	}

	block := types.NewBlockWithHeader(&header).WithBody(txs, nil)
	*b = (RPCBlock)(*block)
	return nil
}*/
