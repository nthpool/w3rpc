package fetcher

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/w3vm"
)

type StreamingFetcher struct {
	rpcFetcher w3vm.Fetcher
}

// Balance implements w3vm.Fetcher.
func (*StreamingFetcher) Balance(common.Address) (*big.Int, error) {
	panic("unimplemented")
}

// Code implements w3vm.Fetcher.
func (*StreamingFetcher) Code(common.Address) ([]byte, error) {
	panic("unimplemented")
}

// HeaderHash implements w3vm.Fetcher.
func (*StreamingFetcher) HeaderHash(*big.Int) (common.Hash, error) {
	panic("unimplemented")
}

// Nonce implements w3vm.Fetcher.
func (*StreamingFetcher) Nonce(common.Address) (uint64, error) {
	panic("unimplemented")
}

// StorageAt implements w3vm.Fetcher.
func (*StreamingFetcher) StorageAt(common.Address, common.Hash) (common.Hash, error) {
	panic("unimplemented")
}

func NewStreamingFetcher(client *w3.Client) w3vm.Fetcher {
	return &StreamingFetcher{}
}
